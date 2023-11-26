package index

import (
	"context"
	"fmt"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"log"
	"time"
)

const (
	INDEXERSTOREGETINDEXERLIST = false
	INDEXERSTOREGETINDEXERGET  = false
	INDEXERSTOREAUD            = false
	EVENT_HANDLER              = false
	INDEXERINDEX               = false
	INDEXERINDEXKEYS           = false
	INDEXERGETINDEXERS         = true
)

func TestDeploymentInformer() {
	clientSet := getRestClient()

	informerFactory := informers.NewSharedInformerFactory(clientSet, time.Second*30)

	// 仅初始化了一个： deploymentInformer
	deploymentInformer := informerFactory.Apps().V1().Deployments()

	// 为deployment资源类型初始化了一个shareindexinformer, 并且这个informer注册到shareindexinformer Factory中去
	deploymentShareIndexInformer := deploymentInformer.Informer()

	if EVENT_HANDLER {
		// add EventHandler function
		fmt.Println("eventHandler-eventHandler")
		eventHandler(deploymentShareIndexInformer)
	}

	//deploymentIndexer := deploymentInformer.Lister()
	// 这个channel并没有初始化
	// var stopChan chan struct{}
	// 初始化一个无缓存channel
	stopChan := make(chan struct{})

	// 启动informer
	informerFactory.Start(stopChan)

	// 等待同步完成
	informerFactory.WaitForCacheSync(stopChan)

	if INDEXERSTOREGETINDEXERLIST {
		// 查询indexer下面目前有哪些deployment List
		fmt.Println("indexerGetIndexerList")
		indexerStoreGetIndexerList(deploymentShareIndexInformer)
	}

	if INDEXERSTOREGETINDEXERGET {
		fmt.Println("indexerGetIndexerGet")
		indexerStoreGetIndexerGet(clientSet, deploymentShareIndexInformer)
	}

	if INDEXERSTOREAUD {
		fmt.Println("indexerAUD")
		indexerStoreAUD(deploymentShareIndexInformer)
	}

	if INDEXERINDEX {
		fmt.Println("indexerIndex")
		indexerIndex(clientSet, deploymentShareIndexInformer)
	}

	if INDEXERINDEXKEYS {
		fmt.Println("indexerIndexKeys")
		indexerIndexKeys(clientSet, deploymentShareIndexInformer)
	}

	if INDEXERGETINDEXERS {
		fmt.Println("indexerGetIndexers")
		indexerGetIndexers(clientSet, deploymentShareIndexInformer)
	}

	// 关闭,传递一个信号过去，让informer/controller关闭
	stopChan <- struct{}{}
	close(stopChan)
}

/*
1. deploymentShareIndexInformer.GetIndexer() 实现了Indexer接口
2. Indexer借口是由type cache struct{} 实现,staging/src/k8s.io/client-go/tools/cache/store.go
3. cache 结构体底层数据事先主要是借助了type threadSafeMap struct{}结构体
type Indexer interface {
	// 在原本的存储的基础上，增加了索引功能
	Store
	Index(indexName string, obj interface{}) ([]interface{}, error)
	IndexKeys(indexName, indexedValue string) ([]string, error)
	ListIndexFuncValues(indexName string) []string
	ByIndex(indexName, indexedValue string) ([]interface{}, error)
	GetIndexers() Indexers
	AddIndexers(newIndexers Indexers) error
}
*/

// 存储添加/更新/删除
func indexerStoreAUD(deploymentShareIndexInformer cache.SharedIndexInformer) {
	//currentObjectKey := deploymentShareIndexInformer.GetIndexer().ListKeys()
	testDeployment := getDeployment()
	testDeploymentObjectKey, err := cache.DeletionHandlingMetaNamespaceKeyFunc(&testDeployment)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Object Key Wait Add: ", testDeploymentObjectKey)

	// 将deployment 插入本地缓存
	err = deploymentShareIndexInformer.GetIndexer().Add(&testDeployment)
	if err != nil {
		fmt.Println("Object Key Add Failed: ", testDeploymentObjectKey, err)
	}

	// 查看插入的deployment在缓存中是否存在
	_, exist, err := deploymentShareIndexInformer.GetIndexer().Get(&testDeployment)
	if err != nil {
		log.Fatal(err)
	}
	if exist {
		fmt.Println("Object Key Exist: ", testDeploymentObjectKey)
	} else {
		fmt.Println("Object Key Not Exist: ", testDeploymentObjectKey)
	}

	// 修改testDeployment副本数,然后更新本地缓存
	var replicas int32 = 2
	testDeployment.Spec.Replicas = &replicas
	err = deploymentShareIndexInformer.GetIndexer().Update(&testDeployment)
	if err != nil {
		log.Fatal("Object Key Update Failed: ", testDeploymentObjectKey, err)
	}
	tmp, _, _ := deploymentShareIndexInformer.GetIndexer().GetByKey(testDeploymentObjectKey)
	dp, _ := tmp.(*appsv1.Deployment)
	fmt.Println("Object Current Replicas: ", *dp.Spec.Replicas)

	// 将本地缓存的testDeployment删除
	err = deploymentShareIndexInformer.GetIndexer().Delete(&testDeployment)
	if err != nil {
		log.Fatal("Object Key Delete Failed: ", testDeploymentObjectKey, err)
	} else {
		fmt.Println("Object Key Delete Success: ", testDeploymentObjectKey)
	}

	// 将本地缓存重新同步一次
	d1 := getDeployment()
	d1.Name = "busybox1"

	d2 := getDeployment()
	d2.Name = "busybox2"

	testDeploymentList := []interface{}{
		&d1,
		&d2,
	}
	err = deploymentShareIndexInformer.GetIndexer().Replace(testDeploymentList, "")
	if err != nil {
		log.Fatal("Indexer Replace Failed")
	} else {
		fmt.Println("Indexer Replace Success")
	}

	// 查看更新本地缓存后的数据: [devops/busybox1 devops/busybox2]
	fmt.Println(deploymentShareIndexInformer.GetIndexer().ListKeys())
}

func indexerStoreGetIndexerList(deploymentShareIndexInformer cache.SharedIndexInformer) {
	// 查询indexer下面目前有哪些deployment List
	for _, dp := range deploymentShareIndexInformer.GetIndexer().List() {
		deployment, _ := dp.(*appsv1.Deployment)
		fmt.Printf("indexer-List: DeploymentNameSpace: %s DeploymentName: %s\n", deployment.Namespace, deployment.Name)
	}

	// 查询indexer下面目前所有的object key
	objectKeys := deploymentShareIndexInformer.GetIndexer().ListKeys()
	for _, objectKey := range objectKeys {
		fmt.Println(objectKey)
	}
}

func indexerStoreGetIndexerGet(clientSet *kubernetes.Clientset, deploymentShareIndexInformer cache.SharedIndexInformer) {
	deployments, err := clientSet.AppsV1().Deployments("").List(context.TODO(), metav1.ListOptions{Limit: 10})
	if err != nil {
		log.Fatal(err)
	}

	// object key => deployment
	var deploymentMap map[string]appsv1.Deployment = make(map[string]appsv1.Deployment, len(deployments.Items))
	var objectKey string

	for _, deployment := range deployments.Items {
		// cache.DeletionHandlingMetaNamespaceKeyFunc 默认的object key 计算函数
		key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(&deployment)
		if err != nil {
			log.Fatal(err)
		}
		deploymentMap[key] = deployment
		objectKey = key
	}

	// 基于object key去本地缓存找
	_, exist, err := deploymentShareIndexInformer.GetIndexer().GetByKey(objectKey)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("GetByKey Namespae: %s DeploymentName: %s Exist: %v \n", deploymentMap[objectKey].Namespace, deploymentMap[objectKey].Name, exist)

	// 基于object去本地缓存找
	// interface{} 需要传指针过去(防止那边改数据)
	tmpDeployment := deploymentMap[objectKey]
	_, exist, err = deploymentShareIndexInformer.GetIndexer().Get(&tmpDeployment)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Get Namespae: %s DeploymentName: %s Exist: %v\n", deploymentMap[objectKey].Namespace, deploymentMap[objectKey].Name, exist)
}

func indexerIndex(clientSet *kubernetes.Clientset, deploymentShareIndexInformer cache.SharedIndexInformer) {
	deployments, err := clientSet.AppsV1().Deployments("").List(context.TODO(), metav1.ListOptions{Limit: 10})
	if err != nil {
		log.Fatal(err)
	}
	// 取出一个deployment
	deployment := deployments.Items[0]
	fmt.Printf("取出Deployment Namespace: %s Name: %s\n", deployment.Namespace, deployment.Name)

	// 返回命名空间索引下 deployment计算出来的索引键下面的所有对象键
	objectList, err := deploymentShareIndexInformer.GetIndexer().Index(cache.NamespaceIndex, &deployment)
	if err != nil {
		log.Fatal(err)
	}
	for _, v := range objectList {
		dp := v.(*appsv1.Deployment)
		fmt.Printf("Namespace: %s Name: %s\n", dp.Namespace, dp.Name)
	}
}

func indexerIndexKeys(clientSet *kubernetes.Clientset, deploymentShareIndexInformer cache.SharedIndexInformer) {
	deployments, err := clientSet.AppsV1().Deployments("").List(context.TODO(), metav1.ListOptions{Limit: 10})
	if err != nil {
		log.Fatal(err)
	}
	// 取出一个deployment
	deployment := deployments.Items[0]
	deploymentObjectKey, err := cache.DeletionHandlingMetaNamespaceKeyFunc(&deployment)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("取出Deployment Namespace: %s Name: %s, ObjectKey: %s\n", deployment.Namespace, deployment.Name, deploymentObjectKey)

	// 在命名空间下，object的namespace就是索引键
	// 待分析 ！！！！！！！！！！！！！！！！！！！
	objectKeyList, err := deploymentShareIndexInformer.GetIndexer().IndexKeys(cache.NamespaceIndex, deploymentObjectKey)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(objectKeyList)
}

func indexerGetIndexers(clientSet *kubernetes.Clientset, deploymentShareIndexInformer cache.SharedIndexInformer) {
	deployments, err := clientSet.AppsV1().Deployments("").List(context.TODO(), metav1.ListOptions{Limit: 10})
	if err != nil {
		log.Fatal(err)
	}
	// 取出一个deployment
	deployment := deployments.Items[0]
	deploymentObjectKey, err := cache.DeletionHandlingMetaNamespaceKeyFunc(&deployment)
	fmt.Printf("取出Deployment Namespace: %s Name: %s, ObjectKey: %s\n", deployment.Namespace, deployment.Name, deploymentObjectKey)

	// 获取索引与索引函数的映射
	indexers := deploymentShareIndexInformer.GetIndexer().GetIndexers()

	nameSpaceIndexFunc, ok := indexers[cache.NamespaceIndex]
	if !ok {
		log.Fatal("namespace not have indexFunc")
	}
	indexValue, err := nameSpaceIndexFunc(&deployment)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(indexValue)
	allObjectKey, err := deploymentShareIndexInformer.GetIndexer().IndexKeys(cache.NamespaceIndex, indexValue[0])
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("索引: %s 使用对应的索引函数计算deployment: %s 基于这个deployment获取它命名空间下面其他所有的对象键: %v", cache.NamespaceIndex, deploymentObjectKey, allObjectKey)
}

// 为底层索引添加索引函数, 只能在informer未启动之前添加
func addIndexers(deploymentShareIndexInformer cache.SharedIndexInformer) {
	fmt.Println(1)
}

// 添加事件处理函数
func eventHandler(deploymentShareIndexInformer cache.SharedIndexInformer) {
	deploymentShareIndexInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, newObj interface{}) {
			deployment, _ := newObj.(*appsv1.Deployment)
			fmt.Printf("Update Namesapce: %s Name: %s\n", deployment.Namespace, deployment.Name)
		},
		AddFunc: func(obj interface{}) {
			deployment, _ := obj.(*appsv1.Deployment)
			fmt.Printf("Add Namesapce: %s Name: %s\n", deployment.Namespace, deployment.Name)
		},
		DeleteFunc: func(obj interface{}) {
			deployment, _ := obj.(*appsv1.Deployment)
			fmt.Printf("Delete Namesapce: %s Name: %s\n", deployment.Namespace, deployment.Name)
		},
	})
}

func indexerAdd(deploymentShareIndexInformer cache.SharedIndexInformer) {
	fmt.Println(1)
}

func indexerUpdate(deploymentShareIndexInformer cache.SharedIndexInformer) {
	fmt.Println(1)
}

func indexerDelete(deploymentShareIndexInformer cache.SharedIndexInformer) {
	fmt.Println(1)
}
