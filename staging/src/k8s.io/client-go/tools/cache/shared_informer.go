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

package cache

import (
	"fmt"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/buffer"
	"k8s.io/utils/clock"

	"k8s.io/klog/v2"
)

// SharedInformer provides eventually consistent linkage of its
// clients to the authoritative state of a given collection of
// objects.  An object is identified by its API group, kind/resource,
// namespace (if any), and name; the `ObjectMeta.UID` is not part of
// an object's ID as far as this contract is concerned.  One
// SharedInformer provides linkage to objects of a particular API
// group and kind/resource.  The linked object collection of a
// SharedInformer may be further restricted to one namespace (if
// applicable) and/or by label selector and/or field selector.
//
// The authoritative state of an object is what apiservers provide
// access to, and an object goes through a strict sequence of states.
// An object state is either (1) present with a ResourceVersion and
// other appropriate content or (2) "absent".
//
// A SharedInformer maintains a local cache --- exposed by GetStore(),
// by GetIndexer() in the case of an indexed informer, and possibly by
// machinery involved in creating and/or accessing the informer --- of
// the state of each relevant object.  This cache is eventually
// consistent with the authoritative state.  This means that, unless
// prevented by persistent communication problems, if ever a
// particular object ID X is authoritatively associated with a state S
// then for every SharedInformer I whose collection includes (X, S)
// eventually either (1) I's cache associates X with S or a later
// state of X, (2) I is stopped, or (3) the authoritative state
// service for X terminates.  To be formally complete, we say that the
// absent state meets any restriction by label selector or field
// selector.
//
// For a given informer and relevant object ID X, the sequence of
// states that appears in the informer's cache is a subsequence of the
// states authoritatively associated with X.  That is, some states
// might never appear in the cache but ordering among the appearing
// states is correct.  Note, however, that there is no promise about
// ordering between states seen for different objects.
//
// The local cache starts out empty, and gets populated and updated
// during `Run()`.
//
// As a simple example, if a collection of objects is henceforth
// unchanging, a SharedInformer is created that links to that
// collection, and that SharedInformer is `Run()` then that
// SharedInformer's cache eventually holds an exact copy of that
// collection (unless it is stopped too soon, the authoritative state
// service ends, or communication problems between the two
// persistently thwart achievement).
//
// As another simple example, if the local cache ever holds a
// non-absent state for some object ID and the object is eventually
// removed from the authoritative state then eventually the object is
// removed from the local cache (unless the SharedInformer is stopped
// too soon, the authoritative state service ends, or communication
// problems persistently thwart the desired result).
//
// The keys in the Store are of the form namespace/name for namespaced
// objects, and are simply the name for non-namespaced objects.
// Clients can use `MetaNamespaceKeyFunc(obj)` to extract the key for
// a given object, and `SplitMetaNamespaceKey(key)` to split a key
// into its constituent parts.
//
// Every query against the local cache is answered entirely from one
// snapshot of the cache's state.  Thus, the result of a `List` call
// will not contain two entries with the same namespace and name.
//
// A client is identified here by a ResourceEventHandler.  For every
// update to the SharedInformer's local cache and for every client
// added before `Run()`, eventually either the SharedInformer is
// stopped or the client is notified of the update.  A client added
// after `Run()` starts gets a startup batch of notifications of
// additions of the objects existing in the cache at the time that
// client was added; also, for every update to the SharedInformer's
// local cache after that client was added, eventually either the
// SharedInformer is stopped or that client is notified of that
// update.  Client notifications happen after the corresponding cache
// update and, in the case of a SharedIndexInformer, after the
// corresponding index updates.  It is possible that additional cache
// and index updates happen before such a prescribed notification.
// For a given SharedInformer and client, the notifications are
// delivered sequentially.  For a given SharedInformer, client, and
// object ID, the notifications are delivered in order.  Because
// `ObjectMeta.UID` has no role in identifying objects, it is possible
// that when (1) object O1 with ID (e.g. namespace and name) X and
// `ObjectMeta.UID` U1 in the SharedInformer's local cache is deleted
// and later (2) another object O2 with ID X and ObjectMeta.UID U2 is
// created the informer's clients are not notified of (1) and (2) but
// rather are notified only of an update from O1 to O2. Clients that
// need to detect such cases might do so by comparing the `ObjectMeta.UID`
// field of the old and the new object in the code that handles update
// notifications (i.e. `OnUpdate` method of ResourceEventHandler).
//
// A client must process each notification promptly; a SharedInformer
// is not engineered to deal well with a large backlog of
// notifications to deliver.  Lengthy processing should be passed off
// to something else, for example through a
// `client-go/util/workqueue`.
//
// A delete notification exposes the last locally known non-absent
// state, except that its ResourceVersion is replaced with a
// ResourceVersion in which the object is actually absent.
type SharedInformer interface {
	// AddEventHandler adds an event handler to the shared informer using the shared informer's resync
	// period.  Events to a single handler are delivered sequentially, but there is no coordination
	// between different handlers.
	// 添加资源事件处理器，当有资源变化时就会通过回调通知使用者
	AddEventHandler(handler ResourceEventHandler)

	// AddEventHandlerWithResyncPeriod adds an event handler to the
	// shared informer with the requested resync period; zero means
	// this handler does not care about resyncs.  The resync operation
	// consists of delivering to the handler an update notification
	// for every object in the informer's local cache; it does not add
	// any interactions with the authoritative storage.  Some
	// informers do no resyncs at all, not even for handlers added
	// with a non-zero resyncPeriod.  For an informer that does
	// resyncs, and for each handler that requests resyncs, that
	// informer develops a nominal resync period that is no shorter
	// than the requested period but may be longer.  The actual time
	// between any two resyncs may be longer than the nominal period
	// because the implementation takes time to do work and there may
	// be competing load and scheduling noise.
	// // 需要周期同步的资源事件处理器
	AddEventHandlerWithResyncPeriod(handler ResourceEventHandler, resyncPeriod time.Duration)

	// GetStore returns the informer's local cache as a Store.
	// 获取一个 Store 对象，前面我们讲解了很多实现 Store 的结构
	GetStore() Store

	// GetController is deprecated, it does nothing useful
	// 主要Deltas/ListWatch/Reflector整合起来
	// 获取一个 Controller，下面会详细介绍，主要是用来将 Reflector 和 DeltaFIFO 组合到一起工作
	GetController() Controller

	// Run starts and runs the shared informer, returning after it stops.
	// The informer will be stopped when stopCh is closed.
	// SharedInformer 的核心实现，启动并运行这个 SharedInformer
	// 当 stopCh 关闭时候，informer 才会退出
	Run(stopCh <-chan struct{})

	// HasSynced returns true if the shared informer's store has been
	// informed by at least one full LIST of the authoritative state
	// of the informer's object collection.  This is unrelated to "resync".
	// 告诉使用者全量的对象是否已经同步到了本地存储中
	HasSynced() bool

	// LastSyncResourceVersion is the resource version observed when last synced with the underlying
	// store. The value returned is not synchronized with access to the underlying store and is not
	// thread-safe.
	LastSyncResourceVersion() string

	// The WatchErrorHandler is called whenever ListAndWatch drops the
	// connection with an error. After calling this handler, the informer
	// will backoff and retry.
	//
	// The default implementation looks at the error type and tries to log
	// the error message at an appropriate level.
	//
	// There's only one handler, so if you call this multiple times, last one
	// wins; calling after the informer has been started returns an error.
	//
	// The handler is intended for visibility, not to e.g. pause the consumers.
	// The handler should return quickly - any expensive processing should be
	// offloaded.
	SetWatchErrorHandler(handler WatchErrorHandler) error
}

// SharedIndexInformer provides add and get Indexers ability based on SharedInformer.
// NewSharedIndexInformer 和 NewSharedInformer 的区别就是SharedIndexInformer可以添加Indexer
type SharedIndexInformer interface {
	SharedInformer

	// AddIndexers add indexers to the informer before it starts.
	// 在启动之前添加 indexers 到 informer 中
	AddIndexers(indexers Indexers) error

	GetIndexer() Indexer
}

// NewSharedInformer creates a new instance for the listwatcher.
func NewSharedInformer(lw ListerWatcher, exampleObject runtime.Object, defaultEventHandlerResyncPeriod time.Duration) SharedInformer {
	return NewSharedIndexInformer(lw, exampleObject, defaultEventHandlerResyncPeriod, Indexers{})
}

// NewSharedIndexInformer creates a new instance for the listwatcher.
// The created informer will not do resyncs if the given
// defaultEventHandlerResyncPeriod is zero.  Otherwise: for each
// handler that with a non-zero requested resync period, whether added
// before or after the informer starts, the nominal resync period is
// the requested resync period rounded up to a multiple of the
// informer's resync checking period.  Such an informer's resync
// checking period is established when the informer starts running,
// and is the maximum of (a) the minimum of the resync periods
// requested before the informer starts and the
// defaultEventHandlerResyncPeriod given here and (b) the constant
// `minimumResyncPeriod` defined in this file.

// 实例化shareindexinformer
// 这里主要是：staging/src/k8s.io/client-go/informers/apps/v1/deployment.go这里面的初始化的时候在调用,因为shareindexinfor是所有资源通用的
// lw = 实例化 pod 类型的 list watcher 对象: cache.NewListWatchFromClient(clientset.CoreV1().RESTClient(), "pods", v1.NamespaceDefault, fields.Everything())
// exampleObject  = &v1.pod{}
// resyncPeriod = NewDeploymentInformer传递的同步时间
func NewSharedIndexInformer(lw ListerWatcher, exampleObject runtime.Object, defaultEventHandlerResyncPeriod time.Duration, indexers Indexers) SharedIndexInformer {
	realClock := &clock.RealClock{}
	// shareIndexInformer 实现了ShareIndexInformer
	sharedIndexInformer := &sharedIndexInformer{
		processor: &sharedProcessor{clock: realClock},
		// DeletionHandlingMetaNamespaceKeyFunc = 在计算object key之前判断一下是否出于删除状态，如果处于删除状态直接返回object key，否则则计算返回
		// indexer: 实际的本地缓存
		indexer:                         NewIndexer(DeletionHandlingMetaNamespaceKeyFunc, indexers),
		listerWatcher:                   lw,            // k8s.io/client-go/informers/apps/v1/deployment.go 中的NewFilteredDeploymentInformer 默认初始化传进来的
		objectType:                      exampleObject, // 需要list/watch对象类型
		resyncCheckPeriod:               defaultEventHandlerResyncPeriod,
		defaultEventHandlerResyncPeriod: defaultEventHandlerResyncPeriod,
		cacheMutationDetector:           NewCacheMutationDetector(fmt.Sprintf("%T", exampleObject)),
		clock:                           realClock,
	}
	return sharedIndexInformer
}

// InformerSynced is a function that can be used to determine if an informer has synced.  This is useful for determining if caches have synced.
type InformerSynced func() bool

const (
	// syncedPollPeriod controls how often you look at the status of your sync funcs
	syncedPollPeriod = 100 * time.Millisecond

	// initialBufferSize is the initial number of event notifications that can be buffered.
	initialBufferSize = 1024
)

// WaitForNamedCacheSync is a wrapper around WaitForCacheSync that generates log messages
// indicating that the caller identified by name is waiting for syncs, followed by
// either a successful or failed sync.
func WaitForNamedCacheSync(controllerName string, stopCh <-chan struct{}, cacheSyncs ...InformerSynced) bool {
	klog.Infof("Waiting for caches to sync for %s", controllerName)

	if !WaitForCacheSync(stopCh, cacheSyncs...) {
		utilruntime.HandleError(fmt.Errorf("unable to sync caches for %s", controllerName))
		return false
	}

	klog.Infof("Caches are synced for %s ", controllerName)
	return true
}

// WaitForCacheSync waits for caches to populate.  It returns true if it was successful, false
// if the controller should shutdown
// callers should prefer WaitForNamedCacheSync()
func WaitForCacheSync(stopCh <-chan struct{}, cacheSyncs ...InformerSynced) bool {
	err := wait.PollImmediateUntil(syncedPollPeriod,
		func() (bool, error) {
			for _, syncFunc := range cacheSyncs {
				if !syncFunc() {
					return false, nil
				}
			}
			return true, nil
		},
		stopCh)
	if err != nil {
		klog.V(2).Infof("stop requested")
		return false
	}

	klog.V(4).Infof("caches populated")
	return true
}

// `*sharedIndexInformer` implements SharedIndexInformer and has three
// main components.  One is an indexed local cache, `indexer Indexer`.
// The second main component is a Controller that pulls
// objects/notifications using the ListerWatcher and pushes them into
// a DeltaFIFO --- whose knownObjects is the informer's local cache
// --- while concurrently Popping Deltas values from that fifo and
// processing them with `sharedIndexInformer::HandleDeltas`.  Each
// invocation of HandleDeltas, which is done with the fifo's lock
// held, processes each Delta in turn.  For each Delta this both
// updates the local cache and stuffs the relevant notification into
// the sharedProcessor.  The third main component is that
// sharedProcessor, which is responsible for relaying those
// notifications to each of the informer's clients.
type sharedIndexInformer struct {
	// 本地的存储
	indexer Indexer

	// 这个controller 里面包含了Reflector,也就是包含了Delta FIFO
	// 在 Controller 中将 Reflector 和 DeltaFIFO 关联了起来
	controller Controller

	// 事件处理器, informer.AddEventHandler注册就是这里
	// 对 ResourceEventHandler 进行了一层层封装，统一由 sharedProcessor 管理
	processor *sharedProcessor

	// 主要用于检测对象是否发生变化
	cacheMutationDetector MutationDetector

	// 用于 Reflector 中真正执行 ListAndWatch 的操作
	listerWatcher ListerWatcher

	// objectType is an example object of the type this informer is
	// expected to handle.  Only the type needs to be right, except
	// that when that is `unstructured.Unstructured` the object's
	// `"apiVersion"` and `"kind"` must also be right.
	// informer 中要处理的对象
	objectType runtime.Object

	// resyncCheckPeriod is how often we want the reflector's resync timer to fire so it can call
	// shouldResync to check if any of our listeners need a resync.
	// 重新同步周期
	resyncCheckPeriod time.Duration

	// defaultEventHandlerResyncPeriod is the default resync period for any handlers added via
	// AddEventHandler (i.e. they don't specify one and just want to use the shared informer's default
	// value).
	// 定期同步周期
	defaultEventHandlerResyncPeriod time.Duration

	// clock allows for testability
	clock clock.Clock

	// 启动、停止标记
	started, stopped bool
	startedLock      sync.Mutex

	// blockDeltas gives a way to stop all event distribution so that a late event handler
	// can safely join the shared informer.
	blockDeltas sync.Mutex

	// Called whenever the ListAndWatch drops the connection with an error.
	watchErrorHandler WatchErrorHandler
}

// dummyController hides the fact that a SharedInformer is different from a dedicated one
// where a caller can `Run`.  The run method is disconnected in this case, because higher
// level logic will decide when to start the SharedInformer and related controller.
// Because returning information back is always asynchronous, the legacy callers shouldn't
// notice any change in behavior.
type dummyController struct {
	informer *sharedIndexInformer
}

func (v *dummyController) Run(stopCh <-chan struct{}) {
}

func (v *dummyController) HasSynced() bool {
	return v.informer.HasSynced()
}

func (v *dummyController) LastSyncResourceVersion() string {
	return ""
}

type updateNotification struct {
	oldObj interface{}
	newObj interface{}
}

type addNotification struct {
	newObj interface{}
}

type deleteNotification struct {
	oldObj interface{}
}

// set error handler
func (s *sharedIndexInformer) SetWatchErrorHandler(handler WatchErrorHandler) error {
	s.startedLock.Lock()
	defer s.startedLock.Unlock()

	if s.started {
		return fmt.Errorf("informer has already started")
	}

	s.watchErrorHandler = handler
	return nil
}

func (s *sharedIndexInformer) Run(stopCh <-chan struct{}) {
	// 创建一个协程，如果收到系统退出的信号就关闭队列
	defer utilruntime.HandleCrash()

	if s.HasStarted() {
		klog.Warningf("The sharedIndexInformer has started, run more than once is not allowed")
		return
	}
	// 初始化Delta FIFO
	fifo := NewDeltaFIFOWithOptions(DeltaFIFOOptions{
		// 本地缓存indexer,这里主要是用与Delta FIFO -> Indexer中
		KnownObjects:          s.indexer,
		EmitDeltaTypeReplaced: true,
		// 如果这里初始化的时候没有指定，会使用默认的计算方式: namespace/name | name
		// KeyFunction:
	})

	// 实例化 informer 其他组件需求的 config 配置集
	cfg := &Config{
		Queue: fifo, // Delta FIFO
		// reflector 内部会依赖这个做 list/watch 操作
		ListerWatcher: s.listerWatcher,
		// list/watch的资源类型： deployment/service/pod等
		ObjectType: s.objectType,
		// 全量同步周期
		FullResyncPeriod: s.resyncCheckPeriod,
		// 错误重试
		RetryOnError: false,
		ShouldResync: s.processor.shouldResync,

		// 这里主要是数据从Pop里面弹出后，要执行这个操作
		// 这个是 reflector 消费 delta fifo 触发的回调方法
		// Delta FIFO数据Pop出来后主要做了二方面事情：
		// 1. 首先更新本地缓存
		// 2. 然后将相应的事件分发到事件处理监听器上(sharedProcessor),然后由监听器去执行我们自定义的这个函数informer.AddEventHandler(cache.ResourceEventHandlerFuncs{})
		Process:           s.HandleDeltas,
		WatchErrorHandler: s.watchErrorHandler,
	}

	func() {
		s.startedLock.Lock()
		defer s.startedLock.Unlock()

		// 构建一个Controller
		s.controller = New(cfg)
		s.controller.(*controller).clock = s.clock
		s.started = true
	}()

	// Separate stop channel because Processor should be stopped strictly after controller
	processorStopCh := make(chan struct{})

	var wg wait.Group
	defer wg.Wait()              // Wait for Processor to stop
	defer close(processorStopCh) // Tell Processor to stop

	// 缓存变更检测, 不用纠结其实现
	wg.StartWithChannel(processorStopCh, s.cacheMutationDetector.Run)

	// 启动 sharedProcessor, s.processor主要是在：k8s.io/client-go/tools/cache/shared_informer.go的NewSharedIndexInformer()中被初始化的
	// s.processor数据主要是在执行：informer.AddEventHandler(cache.ResourceEventHandlerFuncs()}时初始化processorListener(用来执行用户定义的哪些函数)
	// 真正启动了：sharedProcessor，开始去处理Delta FIFO distribute过来的事件
	wg.StartWithChannel(processorStopCh, s.processor.run)

	defer func() {
		s.startedLock.Lock()
		defer s.startedLock.Unlock()
		s.stopped = true // Don't want any new listeners
	}()

	// 启动controller
	s.controller.Run(stopCh)
}

func (s *sharedIndexInformer) HasStarted() bool {
	s.startedLock.Lock()
	defer s.startedLock.Unlock()
	return s.started
}

func (s *sharedIndexInformer) HasSynced() bool {
	s.startedLock.Lock()
	defer s.startedLock.Unlock()

	if s.controller == nil {
		return false
	}
	return s.controller.HasSynced()
}

func (s *sharedIndexInformer) LastSyncResourceVersion() string {
	s.startedLock.Lock()
	defer s.startedLock.Unlock()

	if s.controller == nil {
		return ""
	}
	return s.controller.LastSyncResourceVersion()
}

func (s *sharedIndexInformer) GetStore() Store {
	return s.indexer
}

func (s *sharedIndexInformer) GetIndexer() Indexer {
	return s.indexer
}

func (s *sharedIndexInformer) AddIndexers(indexers Indexers) error {
	s.startedLock.Lock()
	defer s.startedLock.Unlock()

	if s.started {
		return fmt.Errorf("informer has already started")
	}

	return s.indexer.AddIndexers(indexers)
}

func (s *sharedIndexInformer) GetController() Controller {
	return &dummyController{informer: s}
}

// sharedIndexInformer 是支持动态添加 ResourceEventHandler 事件方法
// 根据传入的 handler 对象构建 listener 监听器. 然后把监听器加到 listeners 数组里, 并启动 run 和 pop 两个协程
// 主要用于注册用户自定义资源处理函数：informer.AddEventHandler(cache.ResourceEventHandlerFuncs{}
func (s *sharedIndexInformer) AddEventHandler(handler ResourceEventHandler) {
	s.AddEventHandlerWithResyncPeriod(handler, s.defaultEventHandlerResyncPeriod)
}

func determineResyncPeriod(desired, check time.Duration) time.Duration {
	if desired == 0 {
		return desired
	}
	if check == 0 {
		klog.Warningf("The specified resyncPeriod %v is invalid because this shared informer doesn't support resyncing", desired)
		return 0
	}
	if desired < check {
		klog.Warningf("The specified resyncPeriod %v is being increased to the minimum resyncCheckPeriod %v", desired, check)
		return check
	}
	return desired
}

const minimumResyncPeriod = 1 * time.Second

func (s *sharedIndexInformer) AddEventHandlerWithResyncPeriod(handler ResourceEventHandler, resyncPeriod time.Duration) {
	s.startedLock.Lock()
	defer s.startedLock.Unlock()

	if s.stopped {
		klog.V(2).Infof("Handler %v was not added to shared informer because it has stopped already", handler)
		return
	}

	if resyncPeriod > 0 {
		// 最少一秒
		if resyncPeriod < minimumResyncPeriod {
			klog.Warningf("resyncPeriod %v is too small. Changing it to the minimum allowed value of %v", resyncPeriod, minimumResyncPeriod)
			resyncPeriod = minimumResyncPeriod
		}
		if resyncPeriod < s.resyncCheckPeriod {
			if s.started {
				klog.Warningf("resyncPeriod %v is smaller than resyncCheckPeriod %v and the informer has already started. Changing it to %v", resyncPeriod, s.resyncCheckPeriod, s.resyncCheckPeriod)
				resyncPeriod = s.resyncCheckPeriod
			} else {
				// if the event handler's resyncPeriod is smaller than the current resyncCheckPeriod, update
				// resyncCheckPeriod to match resyncPeriod and adjust the resync periods of all the listeners
				// accordingly
				s.resyncCheckPeriod = resyncPeriod
				// 设置同步时间
				s.processor.resyncCheckPeriodChanged(resyncPeriod)
			}
		}
	}

	// handler = 事件处理方法 = cache.ResourceEventHandlerFuncs
	listener := newProcessListener(handler, resyncPeriod, determineResyncPeriod(resyncPeriod, s.resyncCheckPeriod), s.clock.Now(), initialBufferSize)

	// 当shareIndexInformer没有启动的时候,直接把事件监听器加进去
	if !s.started {
		s.processor.addListener(listener)
		return
	}

	// in order to safely join, we have to
	// 1. stop sending add/update/delete notifications
	// 2. do a list against the store
	// 3. send synthetic "Add" events to the new handler
	// 4. unblock
	s.blockDeltas.Lock()
	defer s.blockDeltas.Unlock()

	// 把构建的 listener 放到 processor 的 listeners 数组里,并启动两个协程处理 run 和 pop 方法.
	s.processor.addListener(listener)

	// 增加通知, s.indexer.List = 返回当前缓存中所有object的List
	for _, item := range s.indexer.List() {
		// 增加通知，让目前增加的这个事件监听处理器去处理一下本地的缓存
		listener.add(addNotification{newObj: item})
	}
}

// obj = object
// HandleDeltas 用来处理从 DeltaFIFO 拿到的 deltas 事件列表, 然后通知给所有的 lisenter 去处理
// HandleDeltas主要是在Delta FIFO被Pop()时触发
func (s *sharedIndexInformer) HandleDeltas(obj interface{}) error {
	s.blockDeltas.Lock()
	defer s.blockDeltas.Unlock()

	// from oldest to newest
	// 拿到deltas事件列表, 然后通知给所有的lisenter去处理
	for _, d := range obj.(Deltas) {
		switch d.Type {
		case Sync, Replaced, Added, Updated:
			s.cacheMutationDetector.AddObject(d.Object)
			// old = 从缓存中拿到的object
			// exists = 本地缓存是否存在这个object
			if old, exists, err := s.indexer.Get(d.Object); err == nil && exists {
				// 当本地存在这个object的时候(本地的是老的),更新本地缓存
				// 更新本地缓存的同时，还会更新本地的索引
				if err := s.indexer.Update(d.Object); err != nil {
					return err
				}

				isSync := false
				switch {
				case d.Type == Sync:
					// Sync events are only propagated to listeners that requested resync
					isSync = true
					// Reflector作为生产者将数据发送到Delta FIFO Queue,其中Reflector的list事件对应Delta FIFO的Replaced事件，
					// Reflector的watch的Added/Modified/Deleted事件对应Delta FIFO的Added/Updated/Deleted事件，Reflector的resync事件对应Delta FIFO的sync事件
				case d.Type == Replaced:
					if accessor, err := meta.Accessor(d.Object); err == nil {
						if oldAccessor, err := meta.Accessor(old); err == nil {
							// Replaced events that didn't change resourceVersion are treated as resync events
							// and only propagated to listeners that requested resync
							isSync = accessor.GetResourceVersion() == oldAccessor.GetResourceVersion()
						}
					}
				}
				// 事件的分发, 没有直接使用 delta 结构, 而是使用 Notification 结构体封装了
				// 这里分发后, shareprocessor就可以用我们定义的事件处理函数进行处理了
				s.processor.distribute(updateNotification{oldObj: old, newObj: d.Object}, isSync)
			} else { // 本地缓存不存在
				// 将这次的object更新到本地缓存中去，同时还要更新其索引
				if err := s.indexer.Add(d.Object); err != nil {
					return err
				}
				// 事件的分发, 没有直接使用 delta 结构, 而是使用 Notification 结构体封装了
				// 这里分发后, shareprocessor就可以用我们定义的事件处理函数进行处理了
				s.processor.distribute(addNotification{newObj: d.Object}, false)
			}
		case Deleted:
			// 从本地缓存删除这个object
			if err := s.indexer.Delete(d.Object); err != nil {
				return err
			}
			// 事件的分发, 没有直接使用 delta 结构, 而是使用 Notification 结构体封装了
			// 这里分发后, shareprocessor就可以用我们定义的事件处理函数进行处理了
			s.processor.distribute(deleteNotification{oldObj: d.Object}, false)
		}
	}
	return nil
}

// sharedProcessor has a collection of processorListener and can
// distribute a notification object to its listeners.  There are two
// kinds of distribute operations.  The sync distributions go to a
// subset of the listeners that (a) is recomputed in the occasional
// calls to shouldResync and (b) every listener is initially put in.
// The non-sync distributions go to every listener.
type sharedProcessor struct {
	listenersStarted bool
	listenersLock    sync.RWMutex

	// 事件处理器集合,可以动态增加
	listeners        []*processorListener
	syncingListeners []*processorListener
	clock            clock.Clock
	wg               wait.Group
}

// 把构建的 listener 放到 processor 的 listeners 数组里,并启动两个协程处理 run 和 pop 方法
func (p *sharedProcessor) addListener(listener *processorListener) {
	p.listenersLock.Lock()
	defer p.listenersLock.Unlock()

	// 将shareProcessor的listeners和syncingListeners初始化
	// listener 包含了我们定义的事件处理方法
	p.addListenerLocked(listener)

	if p.listenersStarted {
		// 事件处理器启动
		p.wg.Start(listener.run)
		p.wg.Start(listener.pop)
	}
}

func (p *sharedProcessor) addListenerLocked(listener *processorListener) {
	p.listeners = append(p.listeners, listener)
	p.syncingListeners = append(p.syncingListeners, listener)
}

// 事件分发
func (p *sharedProcessor) distribute(obj interface{}, sync bool) {
	p.listenersLock.RLock()
	defer p.listenersLock.RUnlock()

	if sync {
		// 遍历同步类型监听器
		for _, listener := range p.syncingListeners {
			// 将事件分发到 p.nextCh中去处理
			listener.add(obj)
		}
	} else {
		// 遍历监听器
		for _, listener := range p.listeners {
			// 将事件分发到 p.nextCh中去处理
			listener.add(obj)
		}
	}
}

// 启动 sharedProcessor 处理器, 遍历所有的 listeners 监听器, 每个 listener 启动两个协程处理 run 和 pop 方法.
// pop() 监听 addCh 队列把 notification 对象扔到 nextCh 管道里
// run() 对 nextCh 进行监听, 然后根据不同类型调用不同的 ResourceEventHandler 方法
// 让人值得注意的是 pop() 方法, 该方法里对数据的搬运有些绕. 你需要细心推敲下代码, 其过程也只是 addCh -> pendingNotifications -> nextCh 而已
// addCh 和 nextCh 都是无缓冲的管道, 在这里只是用来做通知, 因为不好预设 channel 应该配置多大, 大了浪费, 小了则出现概率阻塞
// 那么如果解决 channel 不定长的问题? k8s 里很多组件都是使用一个 ringbuffer 来实现的元素暂存.
func (p *sharedProcessor) run(stopCh <-chan struct{}) {
	func() {
		p.listenersLock.RLock()
		defer p.listenersLock.RUnlock()
		// 遍历所有 listeners 的 run 和 pop 方法
		for _, listener := range p.listeners {
			p.wg.Start(listener.run)
			p.wg.Start(listener.pop)
		}
		p.listenersStarted = true
	}()

	// 当未收到终止信号前，一直停留在这
	<-stopCh
	p.listenersLock.RLock()
	defer p.listenersLock.RUnlock()
	// 退出收尾操作, 遍历 listeners 执行退出
	for _, listener := range p.listeners {
		close(listener.addCh) // Tell .pop() to stop. .pop() will tell .run() to stop
	}
	// 等待所有关联的协程退出
	p.wg.Wait() // Wait for all .pop() and .run() to stop
}

// shouldResync queries every listener to determine if any of them need a resync, based on each listener's resyncPeriod.
func (p *sharedProcessor) shouldResync() bool {
	p.listenersLock.Lock()
	defer p.listenersLock.Unlock()

	p.syncingListeners = []*processorListener{}

	resyncNeeded := false
	now := p.clock.Now()
	for _, listener := range p.listeners {
		// need to loop through all the listeners to see if they need to resync so we can prepare any listeners that are going to be resyncing.
		if listener.shouldResync(now) {
			resyncNeeded = true
			p.syncingListeners = append(p.syncingListeners, listener)
			listener.determineNextResync(now)
		}
	}
	return resyncNeeded
}

func (p *sharedProcessor) resyncCheckPeriodChanged(resyncCheckPeriod time.Duration) {
	p.listenersLock.RLock()
	defer p.listenersLock.RUnlock()

	for _, listener := range p.listeners {
		resyncPeriod := determineResyncPeriod(listener.requestedResyncPeriod, resyncCheckPeriod)
		listener.setResyncPeriod(resyncPeriod)
	}
}

// processorListener relays notifications from a sharedProcessor to
// one ResourceEventHandler --- using two goroutines, two unbuffered
// channels, and an unbounded ring buffer.  The `add(notification)`
// function sends the given notification to `addCh`.  One goroutine
// runs `pop()`, which pumps notifications from `addCh` to `nextCh`
// using storage in the ring buffer while `nextCh` is not keeping up.
// Another goroutine runs `run()`, which receives notifications from
// `nextCh` and synchronously invokes the appropriate handler method.
//
// processorListener also keeps track of the adjusted requested resync
// period of the listener.
type processorListener struct {
	nextCh chan interface{}
	addCh  chan interface{}

	handler ResourceEventHandler

	// pendingNotifications is an unbounded ring buffer that holds all notifications not yet distributed.
	// There is one per listener, but a failing/stalled listener will have infinite pendingNotifications
	// added until we OOM.
	// TODO: This is no worse than before, since reflectors were backed by unbounded DeltaFIFOs, but
	// we should try to do something better.
	pendingNotifications buffer.RingGrowing

	// requestedResyncPeriod is how frequently the listener wants a
	// full resync from the shared informer, but modified by two
	// adjustments.  One is imposing a lower bound,
	// `minimumResyncPeriod`.  The other is another lower bound, the
	// sharedIndexInformer's `resyncCheckPeriod`, that is imposed (a) only
	// in AddEventHandlerWithResyncPeriod invocations made after the
	// sharedIndexInformer starts and (b) only if the informer does
	// resyncs at all.
	requestedResyncPeriod time.Duration

	// resyncPeriod is the threshold that will be used in the logic
	// for this listener.  This value differs from
	// requestedResyncPeriod only when the sharedIndexInformer does
	// not do resyncs, in which case the value here is zero.  The
	// actual time between resyncs depends on when the
	// sharedProcessor's `shouldResync` function is invoked and when
	// the sharedIndexInformer processes `Sync` type Delta objects.
	resyncPeriod time.Duration

	// nextResync is the earliest time the listener should get a full resync
	nextResync time.Time

	// resyncLock guards access to resyncPeriod and nextResync
	resyncLock sync.Mutex
}

func newProcessListener(handler ResourceEventHandler, requestedResyncPeriod, resyncPeriod time.Duration, now time.Time, bufferSize int) *processorListener {
	ret := &processorListener{
		nextCh:                make(chan interface{}),
		addCh:                 make(chan interface{}),
		handler:               handler,                            // 我们自定义事件处理函数
		pendingNotifications:  *buffer.NewRingGrowing(bufferSize), // default 1024
		requestedResyncPeriod: requestedResyncPeriod,              // NewDeploymentInformer传递的时间
		resyncPeriod:          resyncPeriod,                       // NewDeploymentInformer
	}

	ret.determineNextResync(now)

	return ret
}

// 将事件obj -> addCh
func (p *processorListener) add(notification interface{}) {
	// 把obj写到listener对应的addCh管道里.
	p.addCh <- notification
}

// 从addCh中取出数据放到nextCh中
func (p *processorListener) pop() {
	defer utilruntime.HandleCrash()
	defer close(p.nextCh) // Tell .run() to stop

	var nextCh chan<- interface{}
	var notification interface{}
	for {
		select {
		// 把从addCh获取的对象扔到nextCh里
		// 这里丢到nextCh后，(p *processorListener) run() 就会去处理这个管道里面的数据
		// 当nextCh未初始化时这里不会触发,nextCh = nil
		case nextCh <- notification: // 这里相当于已经分发了事件,让 (p *processorListener) run()去处理
			// Notification dispatched
			var ok bool
			// 从环形队列中获取一个object(由于已经处理了一个事件，这里再去获取事件)
			notification, ok = p.pendingNotifications.ReadOne()
			// ok = false, ringbuffer 没有需要处理的
			if !ok { // Nothing to pop
				// 既然没有事情要做, 那么就设 nextCh 为 nil.
				// 当nextCh为nil时, select忽略该case
				nextCh = nil // Disable this select case
			}
			// 当p.addCh 收到事件通知后，这里触发
			// 从 addCh 获取对象, 如果上一次的 noti 还未扔到 nextCh 里, 那么之后的对象扔到 buffer 里
		case notificationToAdd, ok := <-p.addCh:
			if !ok {
				return
			}
			//  当 notification 为空时, 给 nextCh 一个能用的 channel
			if notification == nil { // No notification to pop (and pendingNotifications is empty)
				// Optimize the case - skip adding to pendingNotifications
				notification = notificationToAdd
				// 这里相当于初始化了, nextCh变成非空了,我们这里操作nextCh就是操作p.nextCh
				nextCh = p.nextCh
			} else { // There is already a notification waiting to be dispatched
				// 这里要获取到第二个事件后，缓存就会有数据了
				p.pendingNotifications.WriteOne(notificationToAdd)
			}
		}
	}
}

func (p *processorListener) run() {
	// this call blocks until the channel is closed.  When a panic happens during the notification
	// we will catch it, **the offending item will be skipped!**, and after a short delay (one second)
	// the next notification will be attempted.  This is usually better than the alternative of never
	// delivering again.
	stopCh := make(chan struct{})
	wait.Until(func() {
		for next := range p.nextCh {
			// 实际上这里就是在执行真正的事件处理函数(本地的缓存已经更新了object和索引)
			// informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			//	AddFunc: onAdd,
			//  UpdateFunc: onUpdate,
			//  DeleteFunc: onDelete,
			//})
			switch notification := next.(type) {
			case updateNotification:
				p.handler.OnUpdate(notification.oldObj, notification.newObj)
			case addNotification:
				p.handler.OnAdd(notification.newObj)
			case deleteNotification:
				p.handler.OnDelete(notification.oldObj)
			default:
				utilruntime.HandleError(fmt.Errorf("unrecognized notification: %T", next))
			}
		}
		// the only way to get here is if the p.nextCh is empty and closed
		close(stopCh)
	}, 1*time.Second, stopCh)
}

// shouldResync deterimines if the listener needs a resync. If the listener's resyncPeriod is 0, this always returns false.
// 当前时间晚于或者等于下次同步时间时返回true
func (p *processorListener) shouldResync(now time.Time) bool {
	p.resyncLock.Lock()
	defer p.resyncLock.Unlock()

	if p.resyncPeriod == 0 {
		return false
	}

	return now.After(p.nextResync) || now.Equal(p.nextResync)
}

func (p *processorListener) determineNextResync(now time.Time) {
	p.resyncLock.Lock()
	defer p.resyncLock.Unlock()

	// 设置下次同步时间
	p.nextResync = now.Add(p.resyncPeriod)
}

func (p *processorListener) setResyncPeriod(resyncPeriod time.Duration) {
	p.resyncLock.Lock()
	defer p.resyncLock.Unlock()

	// 设置同步的周期
	p.resyncPeriod = resyncPeriod
}
