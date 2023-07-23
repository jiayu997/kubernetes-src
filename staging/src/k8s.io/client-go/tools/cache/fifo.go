/*
Copyright 2014 The Kubernetes Authors.

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
	"errors"
	"sync"

	"k8s.io/apimachinery/pkg/util/sets"
)

// PopProcessFunc is passed to Pop() method of Queue interface.
// It is supposed to process the accumulator popped from the queue.
type PopProcessFunc func(interface{}) error

// ErrRequeue may be returned by a PopProcessFunc to safely requeue
// the current item. The value of Err will be returned from Pop.
type ErrRequeue struct {
	// Err is returned by the Pop function
	Err error
}

// ErrFIFOClosed used when FIFO is closed
var ErrFIFOClosed = errors.New("DeltaFIFO: manipulating with closed queue")

func (e ErrRequeue) Error() string {
	if e.Err == nil {
		return "the popped item should be requeued without returning an error"
	}
	return e.Err.Error()
}

// Queue extends Store with a collection of Store keys to "process".
// Every Add, Update, or Delete may put the object's key in that collection.
// A Queue has a way to derive the corresponding key given an accumulator.
// A Queue can be accessed concurrently from multiple goroutines.
// A Queue can be "closed", after which Pop operations return an error.
type Queue interface {
	Store

	// Pop blocks until there is at least one key to process or the
	// Queue is closed.  In the latter case Pop returns with an error.
	// In the former case Pop atomically picks one key to process,
	// removes that (key, accumulator) association from the Store, and
	// processes the accumulator.  Pop returns the accumulator that
	// was processed and the result of processing.  The PopProcessFunc
	// may return an ErrRequeue{inner} and in this case Pop will (a)
	// return that (key, accumulator) association to the Queue as part
	// of the atomic processing and (b) return the inner error from
	// Pop.
	// popProcessFunc 主要用于元素pop弹出后，由这个函数来处理
	Pop(PopProcessFunc) (interface{}, error)

	// AddIfNotPresent puts the given accumulator into the Queue (in
	// association with the accumulator's key) if and only if that key
	// is not already associated with a non-empty accumulator.
	AddIfNotPresent(interface{}) error

	// HasSynced returns true if the first batch of keys have all been
	// popped.  The first batch of keys are those of the first Replace
	// operation if that happened before any Add, AddIfNotPresent,
	// Update, or Delete; otherwise the first batch is empty.
	HasSynced() bool

	// Close the queue
	Close()
}

// Pop is helper function for popping from Queue.
// WARNING: Do NOT use this function in non-test code to avoid races
// unless you really really really really know what you are doing.
func Pop(queue Queue) interface{} {
	var result interface{}
	queue.Pop(func(obj interface{}) error {
		result = obj
		return nil
	})
	return result
}

// FIFO is a Queue in which (a) each accumulator is simply the most
// recently provided object and (b) the collection of keys to process
// is a FIFO.  The accumulators all start out empty, and deleting an
// object from its accumulator empties the accumulator.  The Resync
// operation is a no-op.
//
// Thus: if multiple adds/updates of a single object happen while that
// object's key is in the queue before it has been processed then it
// will only be processed once, and when it is processed the most
// recent version will be processed. This can't be done with a channel
//
// FIFO solves this use case:
//   - You want to process every object (exactly) once.
//   - You want to process the most recent version of the object when you process it.
//   - You do not want to process deleted objects, they should be removed from the queue.
//   - You do not want to periodically reprocess objects.
//

// Compare with DeltaFIFO for other use cases.
type FIFO struct {
	lock sync.RWMutex
	cond sync.Cond

	// We depend on the property that every key in `items` is also in `queue`
	// string = 资源对象key,interface{} = object
	// items = > {"busybox-1": object,"busybox-2": object}
	items map[string]interface{}

	// 队列，资源对象key
	// queue -> ["busybox-1",busybox-2"]
	queue []string

	// populated is true if the first batch of items inserted by Replace() has been populated
	// or Delete/Add/Update was called first.
	populated bool

	// initialPopulationCount is the number of items inserted by the first call of Replace()
	initialPopulationCount int

	// keyFunc is used to make the key used for queued item insertion and retrieval, and should be deterministic.
	// 使用object生成key
	keyFunc KeyFunc

	// Indication the queue is closed.
	// Used to indicate a queue is closed so a control loop can exit when a queue is empty.
	// Currently, not used to gate any of CRUD operations.
	closed bool
}

var (
	// 判断fifo是否为queue
	_ = Queue(&FIFO{}) // FIFO is a Queue
)

// Close the queue.
func (f *FIFO) Close() {
	f.lock.Lock()
	defer f.lock.Unlock()
	f.closed = true
	// 会唤醒睡眠的
	f.cond.Broadcast()
}

// HasSynced returns true if an Add/Update/Delete/AddIfNotPresent are called first,
// or the first batch of items inserted by Replace() has been popped.
func (f *FIFO) HasSynced() bool {
	f.lock.Lock()
	defer f.lock.Unlock()
	// 当已获取 + 获取到的数量=0 时返回true
	return f.populated && f.initialPopulationCount == 0
}

// Add inserts an item, and puts it in the queue. The item is only enqueued
// if it doesn't already exist in the set.

// add 实际上是包含了更新逻辑
func (f *FIFO) Add(obj interface{}) error {
	// 基于obj计算出key
	id, err := f.keyFunc(obj)
	if err != nil {
		return KeyError{obj, err}
	}

	f.lock.Lock()
	defer f.lock.Unlock()

	// 设置为true
	f.populated = true

	// 基于map判断这个key是否存在
	if _, exists := f.items[id]; !exists {
		// 将key添加到queue中
		f.queue = append(f.queue, id)
	}
	// 基于key 更新obj
	f.items[id] = obj
	f.cond.Broadcast()
	return nil
}

// AddIfNotPresent inserts an item, and puts it in the queue. If the item is already
// present in the set, it is neither enqueued nor added to the set.
//
// This is useful in a single producer/consumer scenario so that the consumer can
// safely retry items without contending with the producer and potentially enqueueing
// stale items.
func (f *FIFO) AddIfNotPresent(obj interface{}) error {
	id, err := f.keyFunc(obj)
	if err != nil {
		return KeyError{obj, err}
	}
	f.lock.Lock()
	defer f.lock.Unlock()

	// 仅当items中queue[key] 不存在时，才添加
	f.addIfNotPresent(id, obj)
	return nil
}

// addIfNotPresent assumes the fifo lock is already held and adds the provided
// item to the queue under id if it does not already exist.
func (f *FIFO) addIfNotPresent(id string, obj interface{}) {
	f.populated = true
	// 如果已经存在就返回了
	if _, exists := f.items[id]; exists {
		return
	}
	// 添加queue和更新items
	f.queue = append(f.queue, id)
	f.items[id] = obj
	f.cond.Broadcast()
}

// Update is the same as Add in this implementation.
func (f *FIFO) Update(obj interface{}) error {
	return f.Add(obj)
}

// Delete removes an item. It doesn't add it to the queue, because
// this implementation assumes the consumer only cares about the objects,
// not the order in which they were created/added.
func (f *FIFO) Delete(obj interface{}) error {
	id, err := f.keyFunc(obj)
	if err != nil {
		return KeyError{obj, err}
	}
	f.lock.Lock()
	defer f.lock.Unlock()
	f.populated = true
	// 删除items 中 id的元素，此时queue还是存在这个key的，并没有删除,所以我们看他判断是否存在时的逻辑都是判断items的
	delete(f.items, id)
	return err
}

// List returns a list of all the items.
// 返回items 列表
func (f *FIFO) List() []interface{} {
	f.lock.RLock()
	defer f.lock.RUnlock()
	list := make([]interface{}, 0, len(f.items))
	for _, item := range f.items {
		list = append(list, item)
	}
	return list
}

// ListKeys returns a list of all the keys of the objects currently
// in the FIFO.
// 返回所有的key切片
func (f *FIFO) ListKeys() []string {
	f.lock.RLock()
	defer f.lock.RUnlock()
	list := make([]string, 0, len(f.items))
	for key := range f.items {
		list = append(list, key)
	}
	return list
}

// Get returns the requested item, or sets exists=false.
func (f *FIFO) Get(obj interface{}) (item interface{}, exists bool, err error) {
	key, err := f.keyFunc(obj)
	if err != nil {
		return nil, false, KeyError{obj, err}
	}
	return f.GetByKey(key)
}

// GetByKey returns the requested item, or sets exists=false.
func (f *FIFO) GetByKey(key string) (item interface{}, exists bool, err error) {
	f.lock.RLock()
	defer f.lock.RUnlock()
	item, exists = f.items[key]
	return item, exists, nil
}

// IsClosed checks if the queue is closed
func (f *FIFO) IsClosed() bool {
	f.lock.Lock()
	defer f.lock.Unlock()
	return f.closed
}

// Pop waits until an item is ready and processes it. If multiple items are
// ready, they are returned in the order in which they were added/updated.
// The item is removed from the queue (and the store) before it is processed,
// so if you don't successfully process it, it should be added back with
// AddIfNotPresent(). process function is called under lock, so it is safe
// update data structures in it that need to be in sync with the queue.
func (f *FIFO) Pop(process PopProcessFunc) (interface{}, error) {
	f.lock.Lock()
	defer f.lock.Unlock()
	for {
		// 当queue长度为0的时候，会一直阻塞
		for len(f.queue) == 0 {
			// When the queue is empty, invocation of Pop() is blocked until new item is enqueued.
			// When Close() is called, the f.closed is set and the condition is broadcasted.
			// Which causes this loop to continue and return from the Pop().
			if f.closed {
				return nil, ErrFIFOClosed
			}
			// 只有Add和AddIfNotPresent 才会唤醒这里
			f.cond.Wait()
		}
		// 0 是最先加入的元素
		id := f.queue[0]
		// 将queue长度缩小，相当于取出一个元素了
		f.queue = f.queue[1:]
		if f.initialPopulationCount > 0 {
			f.initialPopulationCount--
		}
		// 当items中不存在这个元素时，此时会一直循环下去，知道这个items被添加进来
		item, ok := f.items[id]
		if !ok {
			// Item may have been deleted subsequently.
			continue
		}
		// 取出item[id]这个元素
		delete(f.items, id)

		// 执行pop之后的，用户自定义处理函数，把这个obj传递进去了
		err := process(item)
		if e, ok := err.(ErrRequeue); ok {
			f.addIfNotPresent(id, item)
			err = e.Err
		}

		return item, err
	}
}

// Replace will delete the contents of 'f', using instead the given map.
// 'f' takes ownership of the map, you should not reference the map again
// after calling this function. f's queue is reset, too; upon return, it
// will contain the items in the map, in no particular order.
func (f *FIFO) Replace(list []interface{}, resourceVersion string) error {
	// 基于replace传递过来的 list切片，重新构建一个新的items
	items := make(map[string]interface{}, len(list))
	// 计算传递过来的list key
	for _, item := range list {
		key, err := f.keyFunc(item)
		if err != nil {
			return KeyError{item, err}
		}
		items[key] = item
	}

	f.lock.Lock()
	defer f.lock.Unlock()

	// 当没有操作时
	if !f.populated {
		f.populated = true
		f.initialPopulationCount = len(items)
	}

	// 更新items
	f.items = items

	// 切片截取,相当于清空当前的slice了
	f.queue = f.queue[:0]
	// 基于这次传递过来的list，更新queue
	for id := range items {
		f.queue = append(f.queue, id)
	}
	// 只有当queue长度大于>0时，才能唤醒其他进程去处理
	if len(f.queue) > 0 {
		f.cond.Broadcast()
	}
	return nil
}

// Resync will ensure that every object in the Store has its key in the queue.
// This should be a no-op, because that property is maintained by all operations.
func (f *FIFO) Resync() error {
	f.lock.Lock()
	defer f.lock.Unlock()

	// 构建一个sets
	inQueue := sets.NewString()
	for _, id := range f.queue {
		inQueue.Insert(id)
	}

	for id := range f.items {
		// 当queue中少key时，将items中的key添加到queue中
		if !inQueue.Has(id) {
			f.queue = append(f.queue, id)
		}
	}
	if len(f.queue) > 0 {
		f.cond.Broadcast()
	}
	return nil
}

// NewFIFO returns a Store which can be used to queue up items to
// process.
func NewFIFO(keyFunc KeyFunc) *FIFO {
	f := &FIFO{
		items:   map[string]interface{}{},
		queue:   []string{},
		keyFunc: keyFunc,
	}
	f.cond.L = &f.lock
	return f
}
