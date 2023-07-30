package index

import (
	"fmt"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
func TestMultiIndexKeys(t *testing.T) {}

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
