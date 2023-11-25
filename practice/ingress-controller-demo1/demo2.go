package ingress_controller_demo1

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
	INDEXERSTORECURD           = true
	EVENT_HANDLER              = false
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
		fmt.Println("indexerStoreGetIndexerList")
		indexerStoreGetIndexerList(deploymentShareIndexInformer)
	}

	if INDEXERSTOREGETINDEXERGET {
		fmt.Println("indexerStoreGetIndexerGet")
		indexerStoreGetIndexerGet(clientSet, deploymentShareIndexInformer)
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
	fmt.Println(1)
}

func indexerStoreAdd(deploymentShareIndexInformer cache.SharedIndexInformer) {
	fmt.Println(1)
}

func indexerStoreUpdate(deploymentShareIndexInformer cache.SharedIndexInformer) {
	fmt.Println(1)
}

func indexerStoreDelete(deploymentShareIndexInformer cache.SharedIndexInformer) {
	fmt.Println(1)
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

func indexerStoreGetReplace() {
	fmt.Println(1)
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
