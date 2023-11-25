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

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/util/sets"
)

// Indexer extends Store with multiple indices and restricts each
// accumulator to simply hold the current object (and be empty after
// Delete).
//
// There are three kinds of strings here:
//  1. a storage key, as defined in the Store interface,
//  2. a name of an index, and
//  3. an "indexed value", which is produced by an IndexFunc and
//     can be a field value or any other string computed from the object.
type Indexer interface {
	// 在原本的存储的基础上，增加了索引功能
	Store

	// Index returns the stored objects whose set of indexed values
	// intersects the set of indexed values of the given object, for
	// the named index
	Index(indexName string, obj interface{}) ([]interface{}, error)

	// IndexKeys returns the storage keys of the stored objects whose
	// set of indexed values for the named index includes the given
	// indexed value
	IndexKeys(indexName, indexedValue string) ([]string, error)

	// ListIndexFuncValues returns all the indexed values of the given index
	ListIndexFuncValues(indexName string) []string

	// ByIndex returns the stored objects whose set of indexed values
	// for the named index includes the given indexed value
	ByIndex(indexName, indexedValue string) ([]interface{}, error)

	// GetIndexer return the indexers
	// 返回 索引与索引函数的对应关系
	// type Indexers map[string]IndexFunc
	GetIndexers() Indexers

	// AddIndexers adds more indexers to this store.  If you call this after you already have data
	// in the store, the results are undefined.
	// 添加索引
	AddIndexers(newIndexers Indexers) error
}

// IndexFunc knows how to compute the set of indexed values for an object.
// IndexFunc 可以基于ojb生成一个索引健列表
// 索引器函数，用于计算一个资源对象的索引值列表
type IndexFunc func(obj interface{}) ([]string, error)

// IndexFuncToKeyFuncAdapter adapts an indexFunc to a keyFunc.  This is only useful if your index function returns
// unique values for every object.  This conversion can create errors when more than one key is found.  You
// should prefer to make proper key and index functions.
func IndexFuncToKeyFuncAdapter(indexFunc IndexFunc) KeyFunc {
	return func(obj interface{}) (string, error) {
		indexKeys, err := indexFunc(obj)
		if err != nil {
			return "", err
		}
		if len(indexKeys) > 1 {
			return "", fmt.Errorf("too many keys: %v", indexKeys)
		}
		if len(indexKeys) == 0 {
			return "", fmt.Errorf("unexpected empty indexKeys")
		}
		return indexKeys[0], nil
	}
}

const (
	// NamespaceIndex is the lookup name for the most common index function, which is to index by the namespace field.
	NamespaceIndex string = "namespace"
)

/*
MetaNamespaceIndexFunc is a default index function that indexes based on an object's namespace
默认的namespace索引函数,返回是[]string，但是我们可以发现实际上就是一个string
Indices: {
	"namespace": {
		"default": ["pod-1","pod-2"],
		"kube-system": ["pod-3"]
	},
	"nodeName": {"node1": ["pod1"],"node2": ["pod2"]},

	MetaNamespaceIndexFunc 计算某个pod-1的namespace，例如： default(索引键)
}
*/
func MetaNamespaceIndexFunc(obj interface{}) ([]string, error) {
	meta, err := meta.Accessor(obj)
	if err != nil {
		return []string{""}, fmt.Errorf("object has no meta: %v", err)
	}
	return []string{meta.GetNamespace()}, nil
}

// Index maps the indexed value to a set of keys in the store that match on that value
// 索引键与对象键集合的映射
type Index map[string]sets.String // sets.String存了object key

// Indexers maps a name to an IndexFunc
// string 等于索引方式：我们可以更加label或者命名空间去索引,从而获取到索引健列表
// 具体怎么实现，是我们根据IndexFunc去实现
// 索引器名称与 IndexFunc 的映射，相当于存储索引的各种分类
// 存储索引器，key 为索引器名称，value 为索引器的实现函数
//
//	indexers: {
//		"namespace": NamespaceFunc,
//	    "nodeName": NodeNameFunc,
//	}
type Indexers map[string]IndexFunc

/*
Indices maps a name to an Index
索引器名称与 Index 索引的映射
存储缓存器，key 为索引器名称，value 为缓存的数据
Indices: {
	"namespace": {
		"default": ["pod-1","pod-2"],
		"kube-system": ["pod-3"]
	},
	"nodeName": {"node1": ["pod1"],"node2": ["pod2"]},
}
*/

type Indices map[string]Index

/*
// 包含的所有索引器/分类以及对应的实现
Indexers: {
	"namespace": NamespaceIndexFunc,
	"nodeName": NodeNameIndexFunc,
}
*/

/*
// 包含的所有索引分类中所有的索引数据
Indices: {
	//namespace 这个索引分类下的所有索引数据
	"namespace": {
		// Index 就是一个索引键下所有的对象键列表
		"default": ["pod-1", "pod-2"],
		// Index
		"kube-system": ["pod-3"]
	},
	//nodeName 这个索引分类下的所有索引数据(对象键列表)
	"nodeName": {
		// Index
		"node1": ["pod-1"],
		// Index
		"node2": ["pod-2", "pod-3"]
	}
}
*/
// 1. Indexers和Indices都是按照IndexFunc(名字)分组， 每个IndexFunc输出多个IndexKey，产生相同IndexKey的多个对象存储在一个集合中
// 2. IndexKey主要是用于快速查找ObjectKey; 而ObjectKey是对象存储时唯一命名的key(这个key方便在存储中快速找到相应的对象)
