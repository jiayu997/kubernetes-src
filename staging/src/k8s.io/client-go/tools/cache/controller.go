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
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/clock"
)

// This file implements a low-level controller that is used in
// sharedIndexInformer, which is an implementation of
// SharedIndexInformer.  Such informers, in turn, are key components
// in the high level controllers that form the backbone of the
// Kubernetes control plane.  Look at those for examples, or the
// example in
// https://github.com/kubernetes/client-go/tree/master/examples/workqueue
// .

// Config contains all the settings for one of these low-level controllers.
type Config struct {
	// The queue for your objects - has to be a DeltaFIFO due to
	// assumptions in the implementation. Your Process() function
	// should accept the output of this Queue's Pop() method.
	// 资源对象的队列，其实就是一个 DeltaFIFO
	Queue

	// Something that can list and watch your objects.
	// 用来构造 Reflector 的
	ListerWatcher

	// Something that can process a popped Deltas.
	// Delta FIFO Pop出来后，执行这个Process函数
	Process ProcessFunc

	// ObjectType is an example object of the type this controller is
	// expected to handle.  Only the type needs to be right, except
	// that when that is `unstructured.Unstructured` the object's
	// `"apiVersion"` and `"kind"` must also be right.
	// 对象类型，也就是 Reflector 中使用的
	ObjectType runtime.Object

	// FullResyncPeriod is the period at which ShouldResync is considered.
	// 全量同步周期，在 Reflector 中使用
	FullResyncPeriod time.Duration

	// ShouldResync is periodically used by the reflector to determine
	// whether to Resync the Queue. If ShouldResync is `nil` or
	// returns true, it means the reflector should proceed with the
	// resync.
	// Reflector 中是否需要 Resync 操作
	ShouldResync ShouldResyncFunc

	// If true, when Process() returns an error, re-enqueue the object.
	// TODO: add interface to let you inject a delay/backoff or drop
	//       the object completely if desired. Pass the object in
	//       question to this interface as a parameter.  This is probably moot
	//       now that this functionality appears at a higher level.
	// 出现错误是否需要重试
	RetryOnError bool

	// Called whenever the ListAndWatch drops the connection with an error.
	// Watch 返回 err 的回调函数
	WatchErrorHandler WatchErrorHandler

	// WatchListPageSize is the requested chunk size of initial and relist watch lists.
	// Watch 分页大小
	WatchListPageSize int64
}

// ShouldResyncFunc is a type of function that indicates if a reflector should perform a
// resync or not. It can be used by a shared informer to support multiple event handlers with custom
// resync periods.
type ShouldResyncFunc func() bool

// ProcessFunc processes a single object.
type ProcessFunc func(obj interface{}) error

// `*controller` implements Controller
type controller struct {
	config         Config
	reflector      *Reflector
	reflectorMutex sync.RWMutex
	clock          clock.Clock
}

// Controller is a low-level controller that is parameterized by a
// Config and used in sharedIndexInformer.
type Controller interface {
	// Run does two things.  One is to construct and run a Reflector
	// to pump objects/notifications from the Config's ListerWatcher
	// to the Config's Queue and possibly invoke the occasional Resync
	// on that Queue.  The other is to repeatedly Pop from the Queue
	// and process with the Config's ProcessFunc.  Both of these
	// continue until `stopCh` is closed.
	// Run 函数主要做两件事，一件是构造并运行一个 Reflector 反射器，将对象/通知从 Config 的
	// ListerWatcher 送到 Config 的 Queue 队列，并在该队列上调用 Resync 操作
	// 另外一件事就是不断从队列中弹出对象，并使用 Config 的 ProcessFunc 进行处理
	Run(stopCh <-chan struct{})

	// HasSynced delegates to the Config's Queue
	// APIServer 中的资源对象是否同步到了 Store 中
	HasSynced() bool

	// LastSyncResourceVersion delegates to the Reflector when there
	// is one, otherwise returns the empty string
	LastSyncResourceVersion() string
}

// New makes a new Controller from the given Config.
func New(c *Config) Controller {
	ctlr := &controller{
		config: *c,
		clock:  &clock.RealClock{},
	}
	return ctlr
}

// Run begins processing items, and will continue until a value is sent down stopCh or it is closed.
// It's an error to call Run more than once.
// Run blocks; call via go.
// indexer, informer := cache.NewIndexerInformer
// 调用 informer.Run() 其实是调用 controller.Run() 方法,简单说就是 Run() 内部会实例化 reflector 反射器对象, 并启动 reflector 的 List / Watch 监听 apiserver 事件.
// 当拿到 apiserver 的数据事件时, 把数据推到 deltaFIFO 队列中
func (c *controller) Run(stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	go func() {
		<-stopCh
		// 关闭Delta FIFO
		c.config.Queue.Close()
	}()
	r := NewReflector(
		// 从config中取出list/watch
		// 主要是由：/Users/jiayu/Documents/go-dev/src/kubernetes-src/staging/src/k8s.io/client-go/kubernetes/typed/apps/v1/deployment.go 实现的
		c.config.ListerWatcher,
		// 从config取出对象
		c.config.ObjectType,
		// 从config中取出Delta FIFO
		c.config.Queue, // Delta FIFO
		// 从Config中取出全量同步周期
		c.config.FullResyncPeriod,
	)
	// 从config中获取是否需要重新同步
	r.ShouldResync = c.config.ShouldResync
	// 从config中获取分页大小
	r.WatchListPageSize = c.config.WatchListPageSize
	r.clock = c.clock

	// 当初始化的时候，设置了watch出错函数时就配置
	if c.config.WatchErrorHandler != nil {
		r.watchErrorHandler = c.config.WatchErrorHandler
	}

	c.reflectorMutex.Lock()
	// 初始化controller的reflector
	c.reflector = r
	c.reflectorMutex.Unlock()

	var wg wait.Group

	// 这里启动reflector,启动后reflector会根据Lister&Watcher来watch对应的资源(理论上不会watch其他的资源),然后将数据放入Delta FIFO(带有不同的事件类型)
	wg.StartWithChannel(stopCh, r.Run)

	// processLoop 的逻辑是不断从 deltaQueue 里消费事件, 然后 deltaQueue 内部每次 pop 完之前会调用传入的 process 方法. 该方法是由 processDeltas 来实现的
	// 这里主要实现了二个功能：
	// 1. 更新本地缓存
	// 2. 分发事件到我们自定义的事件处理函数里面去
	wait.Until(c.processLoop, time.Second, stopCh)
	wg.Wait()
}

// Returns true once this controller has completed an initial resource listing
func (c *controller) HasSynced() bool {
	return c.config.Queue.HasSynced()
}

func (c *controller) LastSyncResourceVersion() string {
	c.reflectorMutex.RLock()
	defer c.reflectorMutex.RUnlock()
	if c.reflector == nil {
		return ""
	}
	return c.reflector.LastSyncResourceVersion()
}

// processLoop drains the work queue.
// TODO: Consider doing the processing in parallel. This will require a little thought
// to make sure that we don't end up processing the same object multiple times
// concurrently.
//
// TODO: Plumb through the stopCh here (and down to the queue) so that this can
// actually exit when the controller is stopped. Or just give up on this stuff
// ever being stoppable. Converting this whole package to use Context would
// also be helpful.

// 从Delta FIFO从循环Pop出数据
// processLoop 的逻辑是不断从 deltaQueue 里消费事件, 然后 deltaQueue 内部每次 pop 完之前会调用传入的 process 方法. 该方法是由 processDeltas 来实现的
func (c *controller) processLoop() {
	for {
		// c.config.Queue.Pop 就是Delta FIFO中的Pop
		// c.config.Process初始化具体为：staging/src/k8s.io/client-go/tools/cache/shared_informer.go的func (s *sharedIndexInformer) Run(stopCh <-chan struct{}) 中的s.HandleDeltas函数
		// 执行完这个Pop函数后，会去执行s.HandleDeltas函数,这个函数主要功能：
		// 1. 更新本地缓存indexers
		// 2. 分发事件到事件监听器上面,然后由监听器去执行我们自定义事件函数
		// 3. c.config.Process并不是执行我们的自定义函数，而是封装了一层
		// c.config.Process = func (s *sharedIndexInformer) HandleDeltas(obj interface{}) error {}
		obj, err := c.config.Queue.Pop(PopProcessFunc(c.config.Process))
		if err != nil {
			if err == ErrFIFOClosed {
				return
			}
			if c.config.RetryOnError {
				// This is the safe way to re-enqueue.
				c.config.Queue.AddIfNotPresent(obj)
			}
		}
	}
}

// ResourceEventHandler can handle notifications for events that
// happen to a resource. The events are informational only, so you
// can't return an error.  The handlers MUST NOT modify the objects
// received; this concerns not only the top level of structure but all
// the data structures reachable from it.
//   - OnAdd is called when an object is added.
//   - OnUpdate is called when an object is modified. Note that oldObj is the
//     last known state of the object-- it is possible that several changes
//     were combined together, so you can't use this to see every single
//     change. OnUpdate is also called when a re-list happens, and it will
//     get called even if nothing changed. This is useful for periodically
//     evaluating or syncing something.
//   - OnDelete will get the final state of the item if it is known, otherwise
//     it will get an object of type DeletedFinalStateUnknown. This can
//     happen if the watch is closed and misses the delete event and we don't
//     notice the deletion until the subsequent re-list.
type ResourceEventHandler interface {
	// 添加对象回调函数
	OnAdd(obj interface{})
	// 更新对象回调函数
	OnUpdate(oldObj, newObj interface{})
	// 删除对象回调函数
	OnDelete(obj interface{})
}

// ResourceEventHandlerFuncs is an adaptor to let you easily specify as many or
// as few of the notification functions as you want while still implementing
// ResourceEventHandler.  This adapter does not remove the prohibition against
// modifying the objects.
type ResourceEventHandlerFuncs struct {
	AddFunc    func(obj interface{})
	UpdateFunc func(oldObj, newObj interface{})
	DeleteFunc func(obj interface{})
}

// OnAdd calls AddFunc if it's not nil.
func (r ResourceEventHandlerFuncs) OnAdd(obj interface{}) {
	if r.AddFunc != nil {
		r.AddFunc(obj)
	}
}

// OnUpdate calls UpdateFunc if it's not nil.
func (r ResourceEventHandlerFuncs) OnUpdate(oldObj, newObj interface{}) {
	if r.UpdateFunc != nil {
		r.UpdateFunc(oldObj, newObj)
	}
}

// OnDelete calls DeleteFunc if it's not nil.
func (r ResourceEventHandlerFuncs) OnDelete(obj interface{}) {
	if r.DeleteFunc != nil {
		r.DeleteFunc(obj)
	}
}

// FilteringResourceEventHandler applies the provided filter to all events coming
// in, ensuring the appropriate nested handler method is invoked. An object
// that starts passing the filter after an update is considered an add, and an
// object that stops passing the filter after an update is considered a delete.
// Like the handlers, the filter MUST NOT modify the objects it is given.
type FilteringResourceEventHandler struct {
	FilterFunc func(obj interface{}) bool
	Handler    ResourceEventHandler
}

// OnAdd calls the nested handler only if the filter succeeds
func (r FilteringResourceEventHandler) OnAdd(obj interface{}) {
	if !r.FilterFunc(obj) {
		return
	}
	r.Handler.OnAdd(obj)
}

// OnUpdate ensures the proper handler is called depending on whether the filter matches
func (r FilteringResourceEventHandler) OnUpdate(oldObj, newObj interface{}) {
	newer := r.FilterFunc(newObj)
	older := r.FilterFunc(oldObj)
	switch {
	case newer && older:
		r.Handler.OnUpdate(oldObj, newObj)
	case newer && !older:
		r.Handler.OnAdd(newObj)
	case !newer && older:
		r.Handler.OnDelete(oldObj)
	default:
		// do nothing
	}
}

// OnDelete calls the nested handler only if the filter succeeds
func (r FilteringResourceEventHandler) OnDelete(obj interface{}) {
	if !r.FilterFunc(obj) {
		return
	}
	r.Handler.OnDelete(obj)
}

// DeletionHandlingMetaNamespaceKeyFunc checks for
// DeletedFinalStateUnknown objects before calling
// MetaNamespaceKeyFunc.
func DeletionHandlingMetaNamespaceKeyFunc(obj interface{}) (string, error) {
	if d, ok := obj.(DeletedFinalStateUnknown); ok {
		return d.Key, nil
	}
	return MetaNamespaceKeyFunc(obj)
}

// NewInformer returns a Store and a controller for populating the store
// while also providing event notifications. You should only used the returned
// Store for Get/List operations; Add/Modify/Deletes will cause the event
// notifications to be faulty.
//
// Parameters:
//   - lw is list and watch functions for the source of the resource you want to
//     be informed of.
//   - objType is an object of the type that you expect to receive.
//   - resyncPeriod: if non-zero, will re-list this often (you will get OnUpdate
//     calls, even if nothing changed). Otherwise, re-list will be delayed as
//     long as possible (until the upstream source closes the watch or times out,
//     or you stop the controller).
//   - h is the object you want notifications sent to.

// Share Informer 和 Informer 的主要区别就是可以添加多个 EventHandler
// 这里的Informer事件处理器，无法动态增加
// 这里主要是单informer,内部依赖 controller 实现 informer 的功能
func NewInformer(
	lw ListerWatcher,
	objType runtime.Object,
	resyncPeriod time.Duration,
	h ResourceEventHandler,
) (Store, Controller) {
	// This will hold the client state, as we know it.
	// 这里创建的缓存不带索引功能，仅存储数据
	clientState := NewStore(DeletionHandlingMetaNamespaceKeyFunc)

	return clientState, newInformer(lw, objType, resyncPeriod, h, clientState, nil)
}

// NewIndexerInformer returns an Indexer and a Controller for populating the index
// while also providing event notifications. You should only used the returned
// Index for Get/List operations; Add/Modify/Deletes will cause the event
// notifications to be faulty.
//
// Parameters:
//   - lw is list and watch functions for the source of the resource you want to
//     be informed of.
//   - objType is an object of the type that you expect to receive.
//   - resyncPeriod: if non-zero, will re-list this often (you will get OnUpdate
//     calls, even if nothing changed). Otherwise, re-list will be delayed as
//     long as possible (until the upstream source closes the watch or times out,
//     or you stop the controller).
//   - h is the object you want notifications sent to.
//   - indexers is the indexer for the received object type.

// Share Informer 和 Informer 的主要区别就是可以添加多个 EventHandler
// 这里的Informer事件处理器，无法动态增加
// 这里主要是单informer,内部依赖 controller 实现 informer 的功能
// 单单由 NewIndexerInformer 创建的 informer 的实现是比较简单的, 它的内部依赖 controller 实现 informer 的功能.
// controller 又会关联 reflector, deltaFIFO, Store (indexer, threadSafeMap ) 组件之间的协调联动.
func NewIndexerInformer(
	lw ListerWatcher,
	objType runtime.Object,
	resyncPeriod time.Duration,
	h ResourceEventHandler,
	indexers Indexers,
) (Indexer, Controller) {
	// This will hold the client state, as we know it.
	clientState := NewIndexer(DeletionHandlingMetaNamespaceKeyFunc, indexers)

	return clientState, newInformer(lw, objType, resyncPeriod, h, clientState, nil)
}

// TransformFunc allows for transforming an object before it will be processed
// and put into the controller cache and before the corresponding handlers will
// be called on it.
// TransformFunc (similarly to ResourceEventHandler functions) should be able
// to correctly handle the tombstone of type cache.DeletedFinalStateUnknown
//
// The most common usage pattern is to clean-up some parts of the object to
// reduce component memory usage if a given component doesn't care about them.
// given controller doesn't care for them
type TransformFunc func(interface{}) (interface{}, error)

// NewTransformingInformer returns a Store and a controller for populating
// the store while also providing event notifications. You should only used
// the returned Store for Get/List operations; Add/Modify/Deletes will cause
// the event notifications to be faulty.
// The given transform function will be called on all objects before they will
// put put into the Store and corresponding Add/Modify/Delete handlers will
// be invokved for them.
func NewTransformingInformer(
	lw ListerWatcher,
	objType runtime.Object,
	resyncPeriod time.Duration,
	h ResourceEventHandler,
	transformer TransformFunc,
) (Store, Controller) {
	// This will hold the client state, as we know it.
	clientState := NewStore(DeletionHandlingMetaNamespaceKeyFunc)

	return clientState, newInformer(lw, objType, resyncPeriod, h, clientState, transformer)
}

// NewTransformingIndexerInformer returns an Indexer and a controller for
// populating the index while also providing event notifications. You should
// only used the returned Index for Get/List operations; Add/Modify/Deletes
// will cause the event notifications to be faulty.
// The given transform function will be called on all objects before they will
// be put into the Index and corresponding Add/Modify/Delete handlers will
// be invoked for them.
func NewTransformingIndexerInformer(
	lw ListerWatcher,
	objType runtime.Object,
	resyncPeriod time.Duration,
	h ResourceEventHandler,
	indexers Indexers,
	transformer TransformFunc,
) (Indexer, Controller) {
	// This will hold the client state, as we know it.
	clientState := NewIndexer(DeletionHandlingMetaNamespaceKeyFunc, indexers)

	return clientState, newInformer(lw, objType, resyncPeriod, h, clientState, transformer)
}

// newInformer returns a controller for populating the store while also
// providing event notifications.
//
// Parameters
//   - lw is list and watch functions for the source of the resource you want to
//     be informed of.
//   - objType is an object of the type that you expect to receive.
//   - resyncPeriod: if non-zero, will re-list this often (you will get OnUpdate
//     calls, even if nothing changed). Otherwise, re-list will be delayed as
//     long as possible (until the upstream source closes the watch or times out,
//     or you stop the controller).
//   - h is the object you want notifications sent to.
//   - clientState is the store you want to populate
func newInformer(
	lw ListerWatcher,
	objType runtime.Object,
	resyncPeriod time.Duration,
	h ResourceEventHandler,
	clientState Store,
	transformer TransformFunc,
) Controller {
	// This will hold incoming changes. Note how we pass clientState in as a
	// KeyLister, that way resync operations will result in the correct set
	// of update/delete deltas.
	// 实例化 DeltaFIFO 增量队列
	fifo := NewDeltaFIFOWithOptions(DeltaFIFOOptions{
		KnownObjects:          clientState,
		EmitDeltaTypeReplaced: true,
	})

	// 创建 config 集合对象, 内置 fifo, listerwatcher, process 对象
	cfg := &Config{
		Queue:            fifo,
		ListerWatcher:    lw,
		ObjectType:       objType,
		FullResyncPeriod: resyncPeriod,
		RetryOnError:     false,

		Process: func(obj interface{}) error {
			// from oldest to newest
			for _, d := range obj.(Deltas) {
				obj := d.Object
				if transformer != nil {
					var err error
					obj, err = transformer(obj)
					if err != nil {
						return err
					}
				}

				switch d.Type {
				case Sync, Replaced, Added, Updated:
					if old, exists, err := clientState.Get(obj); err == nil && exists {
						if err := clientState.Update(obj); err != nil {
							return err
						}
						h.OnUpdate(old, obj)
					} else {
						if err := clientState.Add(obj); err != nil {
							return err
						}
						h.OnAdd(obj)
					}
				case Deleted:
					if err := clientState.Delete(obj); err != nil {
						return err
					}
					h.OnDelete(obj)
				}
			}
			return nil
		},
	}
	// 实例化 controller 对象, 注意 newinformer 返回的是 controller 方法
	return New(cfg)
}
