package restClient_Ingress_Controller_Demo

import (
	"context"
	"fmt"
	appsV1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/scheme"
	"log"
	"time"
)

func GetDeployments() {
	config := initRestConfig()

	// 初始化GVK信息,scheme.Codecs.WithoutConversion()  resetClien 默认都是这个编码器
	initGVK(config, "apis", appsV1.SchemeGroupVersion, scheme.Codecs.WithoutConversion())

	restClient := initRestClient(config)
	result := &appsV1.DeploymentList{}
	err := restClient.Get().
		Namespace("").
		Resource("deployments").
		VersionedParams(&metav1.ListOptions{Limit: 10}, scheme.ParameterCodec).
		Do(context.TODO()).
		Into(result)
	if err != nil {
		panic(err)
	}

	for _, d := range result.Items {
		fmt.Println(d.Namespace, d.Name)
	}
}

func WatchDeployment() {
	config := initRestConfig()

	// 初始化GVK信息,scheme.Codecs.WithoutConversion()  resetClien 默认都是这个编码器
	initGVK(config, "apis", appsV1.SchemeGroupVersion, scheme.Codecs.WithoutConversion())
	restClient := initRestClient(config)

	watcher, err := restClient.Get().
		Namespace("").
		Resource("deployments").
		VersionedParams(&metav1.ListOptions{
			Limit:               10,
			Watch:               true,
			AllowWatchBookmarks: true,
		}, scheme.ParameterCodec).
		Watch(context.TODO())
	if err != nil {
		panic(err)
	}

	for chanEvent := range watcher.ResultChan() {
		dp, ok := chanEvent.Object.(*appsV1.Deployment)
		if !ok {
			log.Fatal("type error")
		}
		switch chanEvent.Type {
		case watch.Added:
			fmt.Println(watch.Added, dp.Namespace, dp.Name)
		case watch.Deleted:
			fmt.Println(watch.Deleted, dp.Namespace, dp.Name)
		case watch.Modified:
			fmt.Println(watch.Modified, dp.Namespace, dp.Name)
		case watch.Bookmark:
			fmt.Println(watch.Bookmark, dp.Namespace, dp.Name)
		case watch.Error:
			fmt.Println(watch.Error, dp.Namespace, dp.Name)
		default:
			fmt.Println("default")
		}
	}
	time.Sleep(time.Second * 30000)
	watcher.Stop()
}
