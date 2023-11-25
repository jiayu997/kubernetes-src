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
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
)

// Store is a generic object storage and processing interface.  A
// Store holds a map from string keys to accumulators, and has
// operations to add, update, and delete a given object to/from the
// accumulator currently associated with a given key.

// A Store also knows how to extract the key from a given object, so many operations are given only the object.
// In the simplest Store implementations each accumulator is simply
// the last given object, or empty after Delete, and thus the Store's
// behavior is simple storage.
//
// Reflector knows how to watch a server and update a Store.  This
// package provides a variety of implementations of Store.
type Store interface {

	// Add adds the given object to the accumulator associated with the given object's key
	Add(obj interface{}) error

	// Update updates the given object in the accumulator associated with the given object's key
	Update(obj interface{}) error

	// Delete deletes the given object from the accumulator associated with the given object's key
	Delete(obj interface{}) error

	// List returns a list of all the currently non-empty accumulators
	List() []interface{}

	// ListKeys returns a list of all the keys currently associated with non-empty accumulators
	ListKeys() []string

	// Get returns the accumulator associated with the given object's key
	Get(obj interface{}) (item interface{}, exists bool, err error)

	// GetByKey returns the accumulator associated with the given key
	GetByKey(key string) (item interface{}, exists bool, err error)

	// Replace will delete the contents of the store, using instead the given list.
	// Store takes ownership of the list, you should not reference it after calling this function.
	// Replace 使用[]interface{}更新本地缓存，同时会把本地缓存删除
	Replace([]interface{}, string) error

	// Resync is meaningless in the terms appearing here but has
	// meaning in some implementations that have non-trivial
	// additional behavior (e.g., DeltaFIFO).
	Resync() error
}

// KeyFunc knows how to make a key from an object. Implementations should be deterministic.
type KeyFunc func(obj interface{}) (string, error)

// KeyError will be returned any time a KeyFunc gives an error; it includes the object
// at fault.
type KeyError struct {
	Obj interface{}
	Err error
}

// Error gives a human-readable description of the error.
func (k KeyError) Error() string {
	return fmt.Sprintf("couldn't create key for object %+v: %v", k.Obj, k.Err)
}

// Unwrap implements errors.Unwrap
func (k KeyError) Unwrap() error {
	return k.Err
}

// ExplicitKey can be passed to MetaNamespaceKeyFunc if you have the key for
// the object but not the object itself.
// 当我们传入object key时，会返回object key
type ExplicitKey string

// MetaNamespaceKeyFunc is a convenient default KeyFunc which knows how to make
// keys for API objects which implement meta.Interface.
// The key uses the format <namespace>/<name> unless <namespace> is empty, then
// it's just <name>.
//
// TODO: replace key-as-string with a key-as-struct so that this
// packing/unpacking won't be necessary.

// MetaNamespaceKeyFunc object 计算函数
// 默认的一个object key计算函数
func MetaNamespaceKeyFunc(obj interface{}) (string, error) {
	if key, ok := obj.(ExplicitKey); ok {
		return string(key), nil
	}
	meta, err := meta.Accessor(obj)
	if err != nil {
		return "", fmt.Errorf("object has no meta: %v", err)
	}

	// 当object有namespace返回: namespace/name
	if len(meta.GetNamespace()) > 0 {
		return meta.GetNamespace() + "/" + meta.GetName(), nil
	}

	// 当object不存在namespace时,返回name
	return meta.GetName(), nil
}

// SplitMetaNamespaceKey returns the namespace and name that
// MetaNamespaceKeyFunc encoded into key.
//
// TODO: replace key-as-string with a key-as-struct so that this
// packing/unpacking won't be necessary.

// SplitMetaNamespaceKey  返回object_name和object_key
func SplitMetaNamespaceKey(key string) (namespace, name string, err error) {
	parts := strings.Split(key, "/")
	switch len(parts) {
	case 1:
		// name only, no namespace
		return "", parts[0], nil
	case 2:
		// namespace and name
		return parts[0], parts[1], nil
	}

	return "", "", fmt.Errorf("unexpected key format: %q", key)
}

// `*cache` implements Indexer in terms of a ThreadSafeStore and an associated KeyFunc.
// cache 这个结构体只包含了keyFunc和cacheStorage，所以indexer实际的实现还是由cacheStorage来实现的
// cache 实现了Indexer,这个玩意实际上就是缓存
type cache struct {
	// cacheStorage bears the burden of thread safety for the cache
	// 真正底层存放数据的底层结构
	cacheStorage ThreadSafeStore
	// keyFunc is used to make the key for objects stored in and retrieved from items, and
	// should be deterministic.
	keyFunc KeyFunc
}

// cache实现了store
var _ Store = &cache{}

// Add inserts an item into the cache.
func (c *cache) Add(obj interface{}) error {
	// 计算对象的对象健
	key, err := c.keyFunc(obj)
	if err != nil {
		return KeyError{obj, err}
	}
	// 将对象入缓存,同时还会更新索引
	c.cacheStorage.Add(key, obj)
	return nil
}

// Update sets an item in the cache to its updated state.
func (c *cache) Update(obj interface{}) error {
	// 基于obj计算出object key
	key, err := c.keyFunc(obj)
	if err != nil {
		return KeyError{obj, err}
	}
	c.cacheStorage.Update(key, obj)
	return nil
}

// Delete removes an item from the cache.
func (c *cache) Delete(obj interface{}) error {
	key, err := c.keyFunc(obj)
	if err != nil {
		return KeyError{obj, err}
	}
	c.cacheStorage.Delete(key)
	return nil
}

// List returns a list of all the items.
// List is completely threadsafe as long as you treat all items as immutable.
// 返回当前缓存里面所有object的List
func (c *cache) List() []interface{} {
	return c.cacheStorage.List()
}

// ListKeys returns a list of all the keys of the objects currently
// in the cache.
// 返回存储在缓存中所有资源的一个object key List
func (c *cache) ListKeys() []string {
	return c.cacheStorage.ListKeys()
}

// GetIndexers returns the indexers of cache
// 返回GetIndexers
// 返回indexers = 索引与对应的索引函数集合
func (c *cache) GetIndexers() Indexers {
	return c.cacheStorage.GetIndexers()
}

// Index returns a list of items that match on the index function
// Index is thread-safe so long as you treat all items as immutable
/*
indices: {
	"namespace": {"default": {"pod-1","pod-2"},"devops": {"pod-3","pod-4"},},
	"label": {"labelA": {"pod-1","pod-2"}}
}
*/

// 返回indexName索引器下object的索引键下面对应的所有object,例如default下面的pod-1,pod-2
func (c *cache) Index(indexName string, obj interface{}) ([]interface{}, error) {
	return c.cacheStorage.Index(indexName, obj)
}

/*
indexName = 索引器名称
indexValue = 索引健
[]string = 某个索引器下某个索引健对应的对象健集合
作用：返回某索引器下面，某个索引健对应的所有对象健集合

	indices: {
		"namespace": {"default": {"pod-1","pod-2"},"devops": {"pod-3","pod-4"},},
		"label": {"labelA": {"pod-1","pod-2"}}
	}

indexKeys 相当于 传入 indexName=索引器名称(namespace)、indexValue=索引键(default) 然后索取到所有的对象键 ["pod-1","pod-2"]
indexKeys 与 Index区别在于，index是传入的object,索引键还需要在计算一下
*/
func (c *cache) IndexKeys(indexName, indexKey string) ([]string, error) {
	return c.cacheStorage.IndexKeys(indexName, indexKey)
}

// ListIndexFuncValues returns the list of generated values of an Index func
/*
indices: {
	"namespace": {"default": {"pod-1","pod-2"},"devops": {"pod-3","pod-4"},},
	"label": {"labelA": {"pod-1","pod-2"}}
}
返回某个索引器下有哪些索引健, 例如返回namespace索引器下的所有索引键(default,devops)
*/
func (c *cache) ListIndexFuncValues(indexName string) []string {
	return c.cacheStorage.ListIndexFuncValues(indexName)
}

// indexName = 索引器名称
// indexValue = 索引健名称
// 返回某个索引器下某个索引健的object list
func (c *cache) ByIndex(indexName, indexKey string) ([]interface{}, error) {
	return c.cacheStorage.ByIndex(indexName, indexKey)
}

// 添加一个索引器函数, 当索引已经有数据后，无法再添加了
func (c *cache) AddIndexers(newIndexers Indexers) error {
	return c.cacheStorage.AddIndexers(newIndexers)
}

// Get returns the requested item, or sets exists=false.
// Get is completely threadsafe as long as you treat all items as immutable.
// 根据object判断本地缓存是否存在这个obejct
// item = object
func (c *cache) Get(obj interface{}) (item interface{}, exists bool, err error) {
	// 计算obj的object key
	key, err := c.keyFunc(obj)
	if err != nil {
		return nil, false, KeyError{obj, err}
	}
	return c.GetByKey(key)
}

// GetByKey returns the request item, or exists=false.
// GetByKey is completely threadsafe as long as you treat all items as immutable.
// 根据object key 判断本地缓存中时候有这个object
func (c *cache) GetByKey(key string) (item interface{}, exists bool, err error) {
	item, exists = c.cacheStorage.Get(key)
	return item, exists, nil
}

// Replace will delete the contents of 'c', using instead the given list.
// 'c' takes ownership of the list, you should not reference the list again
// after calling this function.
// list = []object
// 基于 []object更新底层缓存
// Replace 使用[]interface{}更新本地缓存，同时会把本地缓存删除
func (c *cache) Replace(list []interface{}, resourceVersion string) error {
	items := make(map[string]interface{}, len(list))
	for _, item := range list {
		// 计算object key
		key, err := c.keyFunc(item)
		if err != nil {
			return KeyError{item, err}
		}
		items[key] = item
	}
	// 基于新的map[object key]object(新的数据) 将底层的所有索引重构
	c.cacheStorage.Replace(items, resourceVersion)
	return nil
}

// Resync is meaningless for one of these
func (c *cache) Resync() error {
	return nil
}

// NewStore returns a Store implemented simply with a map and a lock.
// 这里的indexers为空，即不带索引功能,但是实际上还是存储数据了
func NewStore(keyFunc KeyFunc) Store {
	return &cache{
		cacheStorage: NewThreadSafeStore(Indexers{}, Indices{}),
		keyFunc:      keyFunc,
	}
}

// NewIndexer returns an Indexer implemented simply with a map and a lock.
// deployment/xx informer 默认 附带了一个默认 namespace和MetaNamespaceIndexFunc索引函数
// 位置： vendor/k8s.io/client-go/informers/apps/v1/deployment.go:90
// indexers = cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}
// keyFunc 默认在初始化sharedIndexInformer的时候是 func DeletionHandlingMetaNamespaceKeyFunc(obj interface{}) (string, error) {} staging/src/k8s.io/client-go/tools/cache/controller.go:339
func NewIndexer(keyFunc KeyFunc, indexers Indexers) Indexer {
	return &cache{
		// Indices默认是空的
		cacheStorage: NewThreadSafeStore(indexers, Indices{}),
		keyFunc:      keyFunc,
	}
}
