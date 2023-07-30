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
	"fmt"
	"sync"

	"k8s.io/apimachinery/pkg/util/sets"
)

// ThreadSafeStore is an interface that allows concurrent indexed
// access to a storage backend.  It is like Indexer but does not
// (necessarily) know how to extract the Store key from a given
// object.
//
// TL;DR caveats: you must not modify anything returned by Get or List as it will break
// the indexing feature in addition to not being thread safe.
//
// The guarantees of thread safety provided by List/Get are only valid if the caller
// treats returned items as read-only. For example, a pointer inserted in the store
// through `Add` will be returned as is by `Get`. Multiple clients might invoke `Get`
// on the same key and modify the pointer in a non-thread-safe way. Also note that
// modifying objects stored by the indexers (if any) will *not* automatically lead
// to a re-index. So it's not a good idea to directly modify the objects returned by
// Get/List, in general.
type ThreadSafeStore interface {
	// key = 索引健，默认的索引健计算方式为：namespace/资源名称
	Add(key string, obj interface{})
	Update(key string, obj interface{})
	Delete(key string)
	Get(key string) (item interface{}, exists bool)
	List() []interface{}
	ListKeys() []string
	Replace(map[string]interface{}, string)
	Index(indexName string, obj interface{}) ([]interface{}, error)
	IndexKeys(indexName, indexKey string) ([]string, error)
	ListIndexFuncValues(name string) []string
	ByIndex(indexName, indexKey string) ([]interface{}, error)
	GetIndexers() Indexers

	// AddIndexers adds more indexers to this store.  If you call this after you already have data
	// in the store, the results are undefined.
	AddIndexers(newIndexers Indexers) error
	// Resync is a no-op and is deprecated
	Resync() error
}

// threadSafeMap implements ThreadSafeStore
type threadSafeMap struct {
	lock sync.RWMutex

	// string = object key interface=object
	// 存储资源对象数据，key(对象键) 通过 keyFunc 得到
	// 不要把索引键和对象键搞混了，索引键是用于对象快速查找的；对象键是对象在存储中的唯一命名,对象健是通过名字+对象的方式存储的
	// 这就是真正存储的数据（对象键 -> 对象）,所有资源的数据都是存在这个items里面
	items map[string]interface{}

	// indexers maps a name to an IndexFunc
	// 索引名称 -> keyFunc
	indexers Indexers

	// indices maps a name to an Index
	// 索引名称 -> Index(索引健->对象健集合)
	indices Indices
}

// items: key -> obj
// default/poda poda{}
// default/podb podb{}
// key=索引健,默认的KeyFunc计算是 namespace/资源名称
// 将对象入缓存，同时更新索引
func (c *threadSafeMap) Add(key string, obj interface{}) {
	c.lock.Lock()
	defer c.lock.Unlock()
	// 从对象键获取到obj
	// when key not exist will get nil return
	oldObject := c.items[key]
	// 更新对象健对应的对象
	c.items[key] = obj
	// 更新索引
	c.updateIndices(oldObject, obj, key)
}

// 和Add实际一样的
func (c *threadSafeMap) Update(key string, obj interface{}) {
	c.lock.Lock()
	defer c.lock.Unlock()
	oldObject := c.items[key]
	c.items[key] = obj
	c.updateIndices(oldObject, obj, key)
}

// 将数据从缓存中删除，同时更新索引
func (c *threadSafeMap) Delete(key string) {
	c.lock.Lock()
	defer c.lock.Unlock()
	if obj, exists := c.items[key]; exists {
		// 将obj从所有的索引健对应的对象健中删除
		c.updateIndices(obj, nil, key)

		// 删除object key: pod{}，相当于直接把数据真正删除了,因为哪些索引什么的，实际只能找到object key，最终还是要根据items[object key]获取到object
		delete(c.items, key)
	}
}

// 根据object key 获取到object
func (c *threadSafeMap) Get(key string) (item interface{}, exists bool) {
	c.lock.RLock()
	defer c.lock.RUnlock()
	item, exists = c.items[key]
	return item, exists
}

// 返回当前缓存里面所有object的List
func (c *threadSafeMap) List() []interface{} {
	c.lock.RLock()
	defer c.lock.RUnlock()
	// 这里make了一个新的[]，相当于深拷贝，防止直接访问出去，外边一旦修改，导致原始值被修改
	list := make([]interface{}, 0, len(c.items))
	for _, item := range c.items {
		list = append(list, item)
	}
	return list
}

// ListKeys returns a list of all the keys of the objects currently in the threadSafeMap.
// 返回存储在缓存中所有资源的一个object key List
func (c *threadSafeMap) ListKeys() []string {
	c.lock.RLock()
	defer c.lock.RUnlock()
	list := make([]string, 0, len(c.items))
	for key := range c.items {
		list = append(list, key)
	}
	return list
}

// 基于新的map[object key]object(新的数据) 将底层的所有索引重构
func (c *threadSafeMap) Replace(items map[string]interface{}, resourceVersion string) {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.items = items

	// rebuild any index
	c.indices = Indices{}
	for key, item := range c.items {
		// 根据obj重新构建其索引
		c.updateIndices(nil, item, key)
	}
}

// Index returns a list of items that match the given object on the index function.
// Index is thread-safe so long as you treat all items as immutable.

// Index 是索引的方法了
// IndexName = 索引名称
// obj = ojb
// 作用：从某个索引器下面，根据用户传入的object返回匹配的所有object key list
func (c *threadSafeMap) Index(indexName string, obj interface{}) ([]interface{}, error) {
	c.lock.RLock()
	defer c.lock.RUnlock()

	// 根据索引名称，获取到其索引函数
	indexFunc := c.indexers[indexName]
	if indexFunc == nil {
		return nil, fmt.Errorf("Index with name %s does not exist", indexName)
	}

	// 使用索引函数，计算出obj它会有哪些索引健
	// default -> ["default/pod1","default/pod2"]   当索引健只有一个时(隐含意思：在命名空间索引器下，只有一个default命名空间索引健)
	// default -> ["default/pod1","default/pod2"] kube-system -> ["kube-system/pod3"]  当索引健有二个时(隐含意思：在命名空间索引器下，有二个命名空间索引健：default/kube-system)
	indexedValues, err := indexFunc(obj)
	if err != nil {
		return nil, err
	}

	// 根据索引器名称获取到index,获取到index后，根据索引健更新对象健集合
	index := c.indices[indexName]

	// used to store index_key's object key
	var storeKeySet sets.String
	if len(indexedValues) == 1 {
		// In majority of cases, there is exactly one value matching.
		// Optimize the most common path - deduping is not needed here.
		// 当索引健只有一个时,直接获取到object key集合了
		storeKeySet = index[indexedValues[0]]
	} else {
		// Need to de-dupe the return list.
		// Since multiple keys are allowed, this can happen.
		storeKeySet = sets.String{}
		// 遍历所有的索引健
		for _, indexedValue := range indexedValues {
			// 遍历某个索引健下面的所有对象健
			for key := range index[indexedValue] {
				// 默认情况下，基于不同的索引健，可能会存在二个索引键下面包含了同样的object key,这里sets帮我们进行了去重
				// add object key to different index key map
				storeKeySet.Insert(key)
			}
		}
	}

	list := make([]interface{}, 0, storeKeySet.Len())
	// 遍历所有的对象健
	for storeKey := range storeKeySet {
		// 根据对象健获取其对象
		list = append(list, c.items[storeKey])
	}

	// 这里相当于拿到某个索引器名称下所有的object List
	// 举例：基于命名空间索引器，我这里获取到了其下面的所有资源
	return list, nil
}

// ByIndex returns a list of the items whose indexed values in the given index include the given indexed value
// indexName = 索引器名称
// indexValue = 索引健名称
// 返回某个索引器下某个索引健的object list
func (c *threadSafeMap) ByIndex(indexName, indexedValue string) ([]interface{}, error) {
	c.lock.RLock()
	defer c.lock.RUnlock()

	// 基于索引器名称获取到索引器函数
	indexFunc := c.indexers[indexName]
	if indexFunc == nil {
		return nil, fmt.Errorf("Index with name %s does not exist", indexName)
	}

	// 获取到index
	index := c.indices[indexName]

	// 根据索引健,获取到了所有对象健集合
	set := index[indexedValue]

	// 深拷贝,防止外面操作影响内部
	list := make([]interface{}, 0, set.Len())
	for key := range set {
		list = append(list, c.items[key])
	}
	// 返回对象List
	// 返回某个索引器下某个索引健对应的对象列表
	return list, nil
}

// IndexKeys returns a list of the Store keys of the objects whose indexed values in the given index include the given indexed value.
// IndexKeys is thread-safe so long as you treat all items as immutable.
// indexName = 索引器名称
// indexValue = 索引健
// []string = 某个索引器下某个索引健对应的对象健集合
// 作用：返回某索引器下面，某个索引健对应的所有对象健集合
func (c *threadSafeMap) IndexKeys(indexName, indexedValue string) ([]string, error) {
	c.lock.RLock()
	defer c.lock.RUnlock()

	// 获取到indexFunc
	indexFunc := c.indexers[indexName]
	if indexFunc == nil {
		return nil, fmt.Errorf("Index with name %s does not exist", indexName)
	}

	// 获取到index
	index := c.indices[indexName]

	// 根据索引健返回sets集合
	set := index[indexedValue]

	// 返回某个索引器下某个索引健对应的对象健集合
	return set.List(), nil
}

// 返回某个索引器下有哪些索引健
func (c *threadSafeMap) ListIndexFuncValues(indexName string) []string {
	c.lock.RLock()
	defer c.lock.RUnlock()

	// 根据索引名称,获取index
	index := c.indices[indexName]
	names := make([]string, 0, len(index))
	// 遍历所有的索引健
	for key := range index {
		names = append(names, key)
	}

	// 返回某个索取器下，有哪些索引健
	return names
}

// 返回indexers
func (c *threadSafeMap) GetIndexers() Indexers {
	return c.indexers
}

func (c *threadSafeMap) AddIndexers(newIndexers Indexers) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	// 当有数据后，就无法添加index
	if len(c.items) > 0 {
		return fmt.Errorf("cannot add indexers to running index")
	}

	oldKeys := sets.StringKeySet(c.indexers)
	newKeys := sets.StringKeySet(newIndexers)

	// 判断新旧是否有冲突
	if oldKeys.HasAny(newKeys.List()...) {
		return fmt.Errorf("indexer conflict: %v", oldKeys.Intersection(newKeys))
	}

	// 从新的indexers中添加到当前indexers中去
	for k, v := range newIndexers {
		c.indexers[k] = v
	}
	return nil
}

// updateIndices modifies the objects location in the managed indexes:
// - for create you must provide only the newObj
// - for update you must provide both the oldObj and the newObj
// - for delete you must provide only the oldObj
// updateIndices must be called from a function that already has a lock on the cache
// key = 对象健
// oldObj 在Add时=nil
func (c *threadSafeMap) updateIndices(oldObj interface{}, newObj interface{}, key string) {
	var oldIndexValues, indexValues []string
	var err error
	//  索引名称，索引函数
	for name, indexFunc := range c.indexers {
		if oldObj != nil {
			// 获取对象的索引健([]string,索引健集合)
			oldIndexValues, err = indexFunc(oldObj)
		} else {
			// 将索引健清空
			oldIndexValues = oldIndexValues[:0]
		}
		if err != nil {
			panic(fmt.Errorf("unable to calculate an index entry for key %q on index %q: %v", key, name, err))
		}

		if newObj != nil {
			// 计算新obj的索引健([]string,索引键集合)
			indexValues, err = indexFunc(newObj)
		} else {
			// 删除的时候,将索引健设置为空
			indexValues = indexValues[:0]
		}
		if err != nil {
			panic(fmt.Errorf("unable to calculate an index entry for key %q on index %q: %v", key, name, err))
		}

		// 获取index(索引健->对象健集合)
		// namespace -> Index (default -> ["pod1","pod2"], kube-system -> ["pod3"])
		index := c.indices[name]
		if index == nil {
			index = Index{}
			c.indices[name] = index
		}

		if len(indexValues) == 1 && len(oldIndexValues) == 1 && indexValues[0] == oldIndexValues[0] {
			// We optimize for the most common case where indexFunc returns a single value which has not been changed
			continue
		}
		// 获取老obj的所有索引健
		for _, value := range oldIndexValues {
			// 基于老的obj，遍历所有的索引健，然后将这个对象从对应的索引健的map中删除
			// value = obj计算出来的某一个索引健
			c.deleteKeyFromIndex(key, value, index)
		}
		// 获取新obj的所有索引健
		for _, value := range indexValues {
			// value = obj计算出来的某一个索引健
			// addKeyToIndex 会初始化index的set.strings
			c.addKeyToIndex(key, value, index)
		}
	}
}

// key = 对象健
// indexValue = 索引健名称
// index = 索引健 -> 对象健的map
// 将object key 添加到索引健与对象健的映射关系中
func (c *threadSafeMap) addKeyToIndex(key, indexValue string, index Index) {
	// set = 某个索引健的对象健集合
	set := index[indexValue]
	if set == nil {
		set = sets.String{}
		index[indexValue] = set
	}
	// 将对象健key添加到indexValue索引健的Map中去
	// 将对象健加入对象健集合
	set.Insert(key)
}

// key = 对象健
// indexValue = 索引健名称
// index = 索引健 -> 对象健的map
// 将object key 从索引键与对象健的映射关系中删除
func (c *threadSafeMap) deleteKeyFromIndex(key, indexValue string, index Index) {
	// 获取indexValue索引健对应的所有对象健
	// set = 某个索引健的对象健集合
	set := index[indexValue]
	if set == nil {
		return
	}
	// 从对象健集合中删除key这个对象健
	set.Delete(key)
	// If we don't delete the set when zero, indices with high cardinality
	// short lived resources can cause memory to increase over time from
	// unused empty sets. See `kubernetes/kubernetes/issues/84959`.
	if len(set) == 0 {
		// 当对象健为空了，要将这个索引健删除
		delete(index, indexValue)
	}
}

// 本地缓存没有同步
func (c *threadSafeMap) Resync() error {
	// Nothing to do
	return nil
}

// NewThreadSafeStore creates a new instance of ThreadSafeStore.
func NewThreadSafeStore(indexers Indexers, indices Indices) ThreadSafeStore {
	return &threadSafeMap{
		// 默认items数据是空的
		items: map[string]interface{}{},
		// indexers/indices是空的
		indexers: indexers,
		indices:  indices,
	}
}
