package index

import (
	"fmt"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
	"testing"
)

// 基于namespace和name的索引函数
func testNamespaceWithNameIndexFunc(obj interface{}) ([]string, error) {
	meta, err := meta.Accessor(obj)
	if err != nil {
		return []string{""}, fmt.Errorf("object has no meta: %v", err.Error())
	}
	namespace := meta.GetNamespace()
	name := meta.GetName()

	if len(namespace) >= 0 {
		return []string{namespace + "/", name}, nil
	} else {
		return []string{name}, nil
	}
}

// 使用默认的object key计算函数
func testKeyFunc(obj interface{}) (string, error) {
	return cache.MetaNamespaceKeyFunc(obj)
}

func testLabelIndexFunc(obj interface{}) ([]string, error) {
	meta, err := meta.Accessor(obj)
	if err != nil {
		return []string{""}, fmt.Errorf("object has no meta: %v", err.Error())
	}
	labels := meta.GetLabels()

	if len(labels) == 0 {
		return []string{""}, fmt.Errorf("object has no labels")
	}

	indexKey := make([]string, 0)
	for key, value := range labels {
		indexKey = append(indexKey, key+"/"+value)
	}
	return indexKey, nil
}

func TestGetIndexFuncValues(t *testing.T) {
	// add namespace/label indexers
	index := cache.NewIndexer(testKeyFunc, cache.Indexers{
		cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
		"label":              testLabelIndexFunc,
	})
	pod1 := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "one", Namespace: "default", Labels: map[string]string{"foo": "vlan"}}}
	pod2 := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "two", Namespace: "default", Labels: map[string]string{"foo": "vlan"}}}
	pod3 := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "three", Namespace: "default", Labels: map[string]string{"foo": "bgp"}}}

	// add object to index and update indices/index
	index.Add(pod1)
	index.Add(pod2)
	index.Add(pod3)

	indexers := index.GetIndexers()

	// get all indexName, indexFunc
	for indexName, indexFunc := range indexers {
		_ = indexFunc
		// 返回某个索引器下所有的索引健
		indexKeys := index.ListIndexFuncValues(indexName)
		// 拿到所有的索引健
		for _, indexKey := range indexKeys {
			// 根据索引名称、索引健获取到所有的object key List
			objectKeys, err := index.IndexKeys(indexName, indexKey)
			if err != nil {
				t.Error(err.Error())
			}
			// 获取到对象健
			for _, objectKey := range objectKeys {
				object, exist, err := index.GetByKey(objectKey)
				if err != nil {
					t.Errorf(err.Error())
				}
				if !exist {
					t.Errorf("object key: %s has no object", objectKey)
				}
				t.Logf("indexName: %s indexKey: %s objectKey: %v object: %v\n", indexName, indexKey, objectKeys, object)
			}
		}
	}
}
func TestMultiIndexKeys(t *testing.T) {
	index := cache.NewIndexer(testKeyFunc, cache.Indexers{
		cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
		"label":              testLabelIndexFunc,
	})

	pod1 := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "one", Namespace: "ns1", Labels: map[string]string{"foo": "vlan"}}}
	pod2 := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "two", Namespace: "ns1", Labels: map[string]string{"foo": "vlan"}}}
	pod3 := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "three", Namespace: "ns2", Labels: map[string]string{"foo": "bgp"}}}
	pod4 := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "four", Namespace: "ns2", Labels: map[string]string{"foo": "bgx"}}}

	_ = index.Add(pod1)
	_ = index.Add(pod2)
	_ = index.Add(pod3)
	_ = index.Add(pod4)

	// 预期的索引健和对象健映射集合
	expectIndexKeyObjectKeyMap := map[string]sets.String{}
	expectIndexKeyObjectKeyMap["ns1"] = sets.NewString("ns1/one", "ns1/two")
	expectIndexKeyObjectKeyMap["ns2"] = sets.NewString("ns2/three", "ns2/four")
	expectIndexKeyObjectKeyMap["foo/vlan"] = sets.NewString("ns1/one", "ns1/two")
	expectIndexKeyObjectKeyMap["foo/bgp"] = sets.NewString("ns2/three")
	expectIndexKeyObjectKeyMap["foo/bgx"] = sets.NewString("ns2/four")

	indexers := index.GetIndexers()
	for indexName, indexFunc := range indexers {
		_ = indexFunc
		if indexName != cache.NamespaceIndex && indexName != "label" {
			t.Errorf("indexName: %s not validate", indexName)
		}
		// 返回某个索引器下所有的索引健
		indexKeys := index.ListIndexFuncValues(indexName)

		// 拿到所有的索引健
		for _, indexKey := range indexKeys {
			// 根据索引名称、索引健获取到所有的object key List
			objectKeys, err := index.IndexKeys(indexName, indexKey)
			if err != nil {
				t.Error(err.Error())
			}
			t.Logf("indexName: %s indexKey: %s objectKeys: %v", indexName, indexKey, objectKeys)
		}
	}
	// 检查索引里面数据是否与预期数据相同
	t.Log("now we start check data whether expect")
	{
		for _, indexName := range []string{"namespace", "label"} {
			for indexKey, objectKeys := range expectIndexKeyObjectKeyMap {
				found := sets.String{}
				// 返回某个索引器某个索引健下面所有对象资源
				objectList, err := index.ByIndex(indexName, indexKey)

				// indexName: namespace indexKey: foo/bgx object List: []
				// indexName: namespace indexKey: foo/bgp object List: []
				if len(objectList) <= 0 {
					continue
				}
				// t.Logf("indexName: %s indexKey: %s object List: %v", indexName, indexKey, objectList)
				if err != nil {
					t.Errorf(err.Error())
				}
				for _, object := range objectList {
					namespace := object.(*v1.Pod).Namespace
					name := object.(*v1.Pod).Name
					found.Insert(namespace + "/" + name)
				}
				//t.Log(found.List())
				// 如果found是否为objectKeys
				if !found.Equal(objectKeys) {
					t.Errorf("missing items, index %s, expected %v but found %v", indexKey, objectKeys.List(), found.List())
				}
				t.Logf("indexName: %s indexKey: %s objectList: %v", indexName, indexKey, found.List())
			}
		}
	}

	// 删除pod
	_ = index.Delete(pod3)
	_ = index.Delete(pod4)
	t.Log("index delete pod3/pod4")
	// 此时应该是
	// namespace: index{"ns1": ["ns1/one","ns1/two"]}
	// label: index{"foo/vlan": ["ns1/one","ns1/two"]}
	ns1Pods, err := index.ByIndex("namespace", "ns1")
	ns1PodsObjectKey := make([]string, 0)
	if err != nil {
		t.Errorf("unexpected error: %v", err.Error())
	}
	for _, object := range ns1Pods {
		namespace := object.(*v1.Pod).Namespace
		name := object.(*v1.Pod).Name
		ns1PodsObjectKey = append(ns1PodsObjectKey, namespace+"/"+name)
	}
	if len(ns1PodsObjectKey) != 0 {
		t.Logf("indexName: namespace indexKey: ns1 objectKeys: %v", ns1PodsObjectKey)
	}
}

func TestItems(t *testing.T) {
	items := map[string]interface{}{}

	// x = nil
	x := items["test"]
	t.Logf("%v,%p\n", x, &x)

	// y = nil , ok = false
	y, ok := items["test1"]
	t.Logf("%v,%p,status: %v\n", y, &y, ok)
}

func TestGetItemsKeyFunc(t *testing.T) {
	podList := []v1.Pod{
		v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "one",
				Namespace: "one",
				Labels:    map[string]string{"foo": "bar"},
			},
		},
		v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "two",
				Namespace: "two",
				Labels:    map[string]string{"foo": "bar"},
			},
		},
		v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "three",
				Namespace: "three",
				Labels:    map[string]string{"foo": "three"},
			},
		},
	}
	keyItems := make(map[string]interface{})
	for _, pod := range podList {
		pod := pod
		key, err := testKeyFunc(&pod)
		if err != nil {
			t.Errorf(err.Error())
		}
		keyItems[key] = pod
		t.Log(key, pod)
	}
}
