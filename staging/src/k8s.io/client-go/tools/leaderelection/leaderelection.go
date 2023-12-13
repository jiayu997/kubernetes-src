/*
Copyright 2015 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package leaderelection implements leader election of a set of endpoints.
// It uses an annotation in the endpoints object to store the record of the
// election state. This implementation does not guarantee that only one
// client is acting as a leader (a.k.a. fencing).
//
// A client only acts on timestamps captured locally to infer the state of the
// leader election. The client does not consider timestamps in the leader
// election record to be accurate because these timestamps may not have been
// produced by a local clock. The implemention does not depend on their
// accuracy and only uses their change to indicate that another client has
// renewed the leader lease. Thus the implementation is tolerant to arbitrary
// clock skew, but is not tolerant to arbitrary clock skew rate.
//
// However the level of tolerance to skew rate can be configured by setting
// RenewDeadline and LeaseDuration appropriately. The tolerance expressed as a
// maximum tolerated ratio of time passed on the fastest node to time passed on
// the slowest node can be approximately achieved with a configuration that sets
// the same ratio of LeaseDuration to RenewDeadline. For example if a user wanted
// to tolerate some nodes progressing forward in time twice as fast as other nodes,
// the user could set LeaseDuration to 60 seconds and RenewDeadline to 30 seconds.
//
// While not required, some method of clock synchronization between nodes in the
// cluster is highly recommended. It's important to keep in mind when configuring
// this client that the tolerance to skew rate varies inversely to master
// availability.
//
// Larger clusters often have a more lenient SLA for API latency. This should be
// taken into account when configuring the client. The rate of leader transitions
// should be monitored and RetryPeriod and LeaseDuration should be increased
// until the rate is stable and acceptably low. It's important to keep in mind
// when configuring this client that the tolerance to API latency varies inversely
// to master availability.
//
// DISCLAIMER: this is an alpha API. This library will likely change significantly
// or even be removed entirely in subsequent releases. Depend on this API at
// your own risk.
package leaderelection

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	rl "k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/utils/clock"
)

const (
	JitterFactor = 1.2
)

// NewLeaderElector creates a LeaderElector from a LeaderElectionConfig
func NewLeaderElector(lec LeaderElectionConfig) (*LeaderElector, error) {
	if lec.LeaseDuration <= lec.RenewDeadline {
		return nil, fmt.Errorf("leaseDuration must be greater than renewDeadline")
	}
	if lec.RenewDeadline <= time.Duration(JitterFactor*float64(lec.RetryPeriod)) {
		return nil, fmt.Errorf("renewDeadline must be greater than retryPeriod*JitterFactor")
	}
	if lec.LeaseDuration < 1 {
		return nil, fmt.Errorf("leaseDuration must be greater than zero")
	}
	if lec.RenewDeadline < 1 {
		return nil, fmt.Errorf("renewDeadline must be greater than zero")
	}
	if lec.RetryPeriod < 1 {
		return nil, fmt.Errorf("retryPeriod must be greater than zero")
	}
	if lec.Callbacks.OnStartedLeading == nil {
		return nil, fmt.Errorf("OnStartedLeading callback must not be nil")
	}
	if lec.Callbacks.OnStoppedLeading == nil {
		return nil, fmt.Errorf("OnStoppedLeading callback must not be nil")
	}

	if lec.Lock == nil {
		return nil, fmt.Errorf("Lock must not be nil.")
	}
	le := LeaderElector{
		config:  lec,
		clock:   clock.RealClock{},
		metrics: globalMetricsFactory.newLeaderMetrics(),
	}
	le.metrics.leaderOff(le.config.Name)
	return &le, nil
}

type LeaderElectionConfig struct {
	// Lock is the resource that will be used for locking
	// LeaderElectionConfig.lock 支持保存在以下三种资源中：
	// 1.configmap
	// 2.endpoint
	// 3. lease
	Lock rl.Interface

	// LeaseDuration is the duration that non-leader candidates will
	// wait to force acquire leadership. This is measured against time of
	// last observed ack.
	//
	// A client needs to wait a full LeaseDuration without observing a change to
	// the record before it can attempt to take over. When all clients are
	// shutdown and a new set of clients are started with different names against
	// the same leader record, they must wait the full LeaseDuration before
	// attempting to acquire the lease. Thus LeaseDuration should be as short as
	// possible (within your tolerance for clock skew rate) to avoid a possible
	// long waits in the scenario.
	//
	// Core clients default this value to 15 seconds.
	// 持有锁的时间
	LeaseDuration time.Duration
	// RenewDeadline is the duration that the acting master will retry
	// refreshing leadership before giving up.
	//
	// Core clients default this value to 10 seconds.
	// 在更新租约的超时时间
	RenewDeadline time.Duration
	// RetryPeriod is the duration the LeaderElector clients should wait
	// between tries of actions.
	//
	// Core clients default this value to 2 seconds.
	// 竞争获取锁的时间
	RetryPeriod time.Duration

	// Callbacks are callbacks that are triggered during certain lifecycle
	// events of the LeaderElector
	// 1、OnStartedLeading 启动是执行的业务代码
	// 2、OnStoppedLeading leader停止执行的方法
	// 3、OnNewLeader 当产生新的leader后执行的方法
	Callbacks LeaderCallbacks

	// WatchDog is the associated health checker
	// WatchDog may be null if it's not needed/configured.
	// 进行监控检查
	WatchDog *HealthzAdaptor

	// ReleaseOnCancel should be set true if the lock should be released
	// when the run context is cancelled. If you set this to true, you must
	// ensure all code guarded by this lease has successfully completed
	// prior to cancelling the context, or you may have two processes
	// simultaneously acting on the critical path.
	// leader退出时，是否执行release方法
	ReleaseOnCancel bool

	// Name is the name of the resource lock for debugging
	Name string
}

// LeaderCallbacks are callbacks that are triggered during certain
// lifecycle events of the LeaderElector. These are invoked asynchronously.
//
// possible future callbacks:
//   - OnChallenge()
type LeaderCallbacks struct {
	// OnStartedLeading is called when a LeaderElector client starts leading
	// 开始选举后跳用函数，由用户定义的
	OnStartedLeading func(context.Context)

	// OnStoppedLeading is called when a LeaderElector client stops leading
	// 用户定义的
	OnStoppedLeading func()

	// OnNewLeader is called when the client observes a leader that is
	// not the previously observed leader. This includes the first observed
	// leader when the client starts.
	// 用户定义的
	OnNewLeader func(identity string)
}

// LeaderElector is a leader election client.
type LeaderElector struct {
	config LeaderElectionConfig
	// internal bookkeeping
	observedRecord    rl.LeaderElectionRecord
	observedRawRecord []byte
	observedTime      time.Time
	// used to implement OnNewLeader(), may lag slightly from the
	// value observedRecord.HolderIdentity if the transition has
	// not yet been reported.
	reportedLeader string

	// clock is wrapper around time to allow for less flaky testing
	clock clock.Clock

	// used to lock the observedRecord
	observedRecordLock sync.Mutex

	metrics leaderMetricsAdapter
}

// Run starts the leader election loop. Run will not return
// before leader election loop is stopped by ctx or it has
// stopped holding the leader lease
func (le *LeaderElector) Run(ctx context.Context) {
	defer runtime.HandleCrash()
	// 调用丢失leader函数
	defer func() {
		le.config.Callbacks.OnStoppedLeading()
	}()

	// 不断的进行获得锁，如果获得锁成功则执行后面的方法，否则不断的进行重试
	if !le.acquire(ctx) {
		return // ctx signalled done
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// 获取锁成功，当前进程变为leader，执行回调函数中的业务代码
	go le.config.Callbacks.OnStartedLeading(ctx)

	// 不断的循环进行进行租约的更新，保证锁一直被当前进行持有
	le.renew(ctx)
}

// RunOrDie starts a client with the provided config or panics if the config
// fails to validate. RunOrDie blocks until leader election loop is
// stopped by ctx or it has stopped holding the leader lease
// RunOrDime 会一直阻塞
func RunOrDie(ctx context.Context, lec LeaderElectionConfig) {
	le, err := NewLeaderElector(lec)
	if err != nil {
		panic(err)
	}
	// 健康检查调协
	if lec.WatchDog != nil {
		lec.WatchDog.SetLeaderElection(le)
	}

	// 启动leaderselector
	le.Run(ctx)
}

// GetLeader returns the identity of the last observed leader or returns the empty string if
// no leader has yet been observed.
// This function is for informational purposes. (e.g. monitoring, logs, etc.)
func (le *LeaderElector) GetLeader() string {
	return le.getObservedRecord().HolderIdentity
}

// IsLeader returns true if the last observed leader was this client else returns false.
func (le *LeaderElector) IsLeader() bool {
	// 主要是用identity 来区分不通的锁
	return le.getObservedRecord().HolderIdentity == le.config.Lock.Identity()
}

// acquire loops calling tryAcquireOrRenew and returns true immediately when tryAcquireOrRenew succeeds.
// Returns false if ctx signals done.
func (le *LeaderElector) acquire(ctx context.Context) bool {
	// 基于传过来的ctx，生产一个新的ctx
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	succeeded := false
	// 获取锁的编号：Identity，ResourceLockConfig.Identity
	desc := le.config.Lock.Describe()
	klog.Infof("attempting to acquire leader lease %v...", desc)

	// 循环等待
	wait.JitterUntil(func() {
		// 尝试去获取锁/或者更新租期
		succeeded = le.tryAcquireOrRenew(ctx)
		le.maybeReportTransition()
		if !succeeded {
			klog.V(4).Infof("failed to acquire lease %v", desc)
			return
		}
		le.config.Lock.RecordEvent("became leader")
		le.metrics.leaderOn(le.config.Name)
		klog.Infof("successfully acquired lease %v", desc)
		cancel()
	}, le.config.RetryPeriod, JitterFactor, true, ctx.Done())
	return succeeded
}

// renew loops calling tryAcquireOrRenew and returns immediately when tryAcquireOrRenew fails or ctx signals done.
func (le *LeaderElector) renew(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	wait.Until(func() {
		timeoutCtx, timeoutCancel := context.WithTimeout(ctx, le.config.RenewDeadline)
		defer timeoutCancel()
		err := wait.PollImmediateUntil(le.config.RetryPeriod, func() (bool, error) {
			return le.tryAcquireOrRenew(timeoutCtx), nil
		}, timeoutCtx.Done())

		le.maybeReportTransition()
		desc := le.config.Lock.Describe()
		if err == nil {
			klog.V(5).Infof("successfully renewed lease %v", desc)
			return
		}
		le.config.Lock.RecordEvent("stopped leading")
		le.metrics.leaderOff(le.config.Name)
		klog.Infof("failed to renew lease %v: %v", desc, err)
		cancel()
	}, le.config.RetryPeriod, ctx.Done())

	// if we hold the lease, give it up
	if le.config.ReleaseOnCancel {
		le.release()
	}
}

// release attempts to release the leader lease if we have acquired it.
func (le *LeaderElector) release() bool {
	if !le.IsLeader() {
		return true
	}
	now := metav1.Now()
	leaderElectionRecord := rl.LeaderElectionRecord{
		LeaderTransitions:    le.observedRecord.LeaderTransitions,
		LeaseDurationSeconds: 1,
		RenewTime:            now,
		AcquireTime:          now,
	}
	if err := le.config.Lock.Update(context.TODO(), leaderElectionRecord); err != nil {
		klog.Errorf("Failed to release lock: %v", err)
		return false
	}

	le.setObservedRecord(&leaderElectionRecord)
	return true
}

// tryAcquireOrRenew tries to acquire a leader lease if it is not already acquired,
// else it tries to renew the lease if it has already been acquired. Returns true
// on success else returns false.
func (le *LeaderElector) tryAcquireOrRenew(ctx context.Context) bool {
	now := metav1.Now()
	leaderElectionRecord := rl.LeaderElectionRecord{
		// 唯一标识
		HolderIdentity:       le.config.Lock.Identity(),
		LeaseDurationSeconds: int(le.config.LeaseDuration / time.Second),
		RenewTime:            now,
		AcquireTime:          now,
	}

	// 1. obtain or create the ElectionRecord
	// 从k8s资源中获取原有的锁对象, 这二个对象实际上都一样，后边这个是前边这个的部分序列化
	oldLeaderElectionRecord, oldLeaderElectionRawRecord, err := le.config.Lock.Get(ctx)
	if err != nil {
		if !errors.IsNotFound(err) {
			klog.Errorf("error retrieving resource lock %v: %v", le.config.Lock.Describe(), err)
			return false
		}

		// 当没有这个lease对象的时候，创建这个lease对象
		if err = le.config.Lock.Create(ctx, leaderElectionRecord); err != nil {
			klog.Errorf("error initially creating leader election record: %v", err)
			return false
		}

		// 对使用leaderElectionRecord 深拷贝否复制给 LeaderElector.observedRecord
		le.setObservedRecord(&leaderElectionRecord)

		return true
	}

	// 2. Record obtained, check the Identity & Time
	// 当集群中有这个lease对象存在的时候判断二个是否一致
	if !bytes.Equal(le.observedRawRecord, oldLeaderElectionRawRecord) {
		le.setObservedRecord(oldLeaderElectionRecord)

		le.observedRawRecord = oldLeaderElectionRawRecord
	}

	// 判断锁是否过期了
	if len(oldLeaderElectionRecord.HolderIdentity) > 0 &&
		le.observedTime.Add(le.config.LeaseDuration).After(now.Time) &&

		// 判断是自己是否leader
		!le.IsLeader() {
		klog.V(4).Infof("lock is held by %v and has not yet expired", oldLeaderElectionRecord.HolderIdentity)
		return false
	}

	// 3. We're going to try to update. The leaderElectionRecord is set to it's default
	// here. Let's correct it before updating.
	// 当我们没过期
	if le.IsLeader() {
		// 自己本身就是leader则不需要更新AcquireTime和LeaderTransitions
		leaderElectionRecord.AcquireTime = oldLeaderElectionRecord.AcquireTime
		leaderElectionRecord.LeaderTransitions = oldLeaderElectionRecord.LeaderTransitions
	} else {
		// 首次自己变为leader则更新leader的更换次数
		leaderElectionRecord.LeaderTransitions = oldLeaderElectionRecord.LeaderTransitions + 1
	}

	// update the lock itself
	if err = le.config.Lock.Update(ctx, leaderElectionRecord); err != nil {
		klog.Errorf("Failed to update lock: %v", err)
		return false
	}

	// 更新锁资源，这里如果在 Get 和 Update 之间有变化，将会更新失败
	le.setObservedRecord(&leaderElectionRecord)
	return true
}

func (le *LeaderElector) maybeReportTransition() {
	if le.observedRecord.HolderIdentity == le.reportedLeader {
		return
	}
	le.reportedLeader = le.observedRecord.HolderIdentity
	if le.config.Callbacks.OnNewLeader != nil {
		go le.config.Callbacks.OnNewLeader(le.reportedLeader)
	}
}

// Check will determine if the current lease is expired by more than timeout.
func (le *LeaderElector) Check(maxTolerableExpiredLease time.Duration) error {
	if !le.IsLeader() {
		// Currently not concerned with the case that we are hot standby
		return nil
	}
	// If we are more than timeout seconds after the lease duration that is past the timeout
	// on the lease renew. Time to start reporting ourselves as unhealthy. We should have
	// died but conditions like deadlock can prevent this. (See #70819)
	if le.clock.Since(le.observedTime) > le.config.LeaseDuration+maxTolerableExpiredLease {
		return fmt.Errorf("failed election to renew leadership on lease %s", le.config.Name)
	}

	return nil
}

// setObservedRecord will set a new observedRecord and update observedTime to the current time.
// Protect critical sections with lock.
func (le *LeaderElector) setObservedRecord(observedRecord *rl.LeaderElectionRecord) {
	le.observedRecordLock.Lock()
	defer le.observedRecordLock.Unlock()

	le.observedRecord = *observedRecord
	le.observedTime = le.clock.Now()
}

// getObservedRecord returns observersRecord.
// Protect critical sections with lock.
func (le *LeaderElector) getObservedRecord() rl.LeaderElectionRecord {
	le.observedRecordLock.Lock()
	defer le.observedRecordLock.Unlock()

	return le.observedRecord
}
