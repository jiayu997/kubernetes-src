/*
Copyright The Kubernetes Authors.

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

// Code generated by informer-gen. DO NOT EDIT.

package v1

import (
	"context"
	time "time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	watch "k8s.io/apimachinery/pkg/watch"
	internalinterfaces "k8s.io/client-go/informers/internalinterfaces"
	kubernetes "k8s.io/client-go/kubernetes"
	v1 "k8s.io/client-go/listers/apps/v1"
	cache "k8s.io/client-go/tools/cache"
)

// DeploymentInformer provides access to a shared informer and lister for
// Deployments.
type DeploymentInformer interface {
	Informer() cache.SharedIndexInformer
	Lister() v1.DeploymentLister
}

// kubernetes上的每一个资源都实现了Informer机制，每一个Informer上都会实现Informer和Lister方法，
type deploymentInformer struct {
	factory          internalinterfaces.SharedInformerFactory
	tweakListOptions internalinterfaces.TweakListOptionsFunc
	namespace        string
}

// NewDeploymentInformer constructs a new informer for Deployment type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewDeploymentInformer(client kubernetes.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers) cache.SharedIndexInformer {
	return NewFilteredDeploymentInformer(client, namespace, resyncPeriod, indexers, nil)
}

// NewFilteredDeploymentInformer constructs a new informer for Deployment type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewFilteredDeploymentInformer(client kubernetes.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers, tweakListOptions internalinterfaces.TweakListOptionsFunc) cache.SharedIndexInformer {
	// 这里调用了：vendor/k8s.io/client-go/tools/cache/shared_informer.go
	return cache.NewSharedIndexInformer(
		// 初始化List和watch,这里可以设置一些过滤options
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				// 返回deploymentList
				return client.AppsV1().Deployments(namespace).List(context.TODO(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.AppsV1().Deployments(namespace).Watch(context.TODO(), options)
			},
		},
		&appsv1.Deployment{}, // 仅对deployment list/watch
		resyncPeriod,         // 同步时间
		indexers,			  // 缓存，默认是附带一个命名空间索引
	)
}

func (f *deploymentInformer) defaultInformer(client kubernetes.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
	// 默认的informer附带的缓存默认具有一个命名空间索引,索引健计算默认使用namespace/name计算
	// 这里主要是对某类资源初始化了一个shareindexinformer
	return NewFilteredDeploymentInformer(client, f.namespace, resyncPeriod, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, f.tweakListOptions)
}

func (f *deploymentInformer) Informer() cache.SharedIndexInformer {
	// f.defaultInformer = cache.SharedIndexInformer
	// vendor/k8s.io/client-go/informers/factory.go:207  实际调用了工厂的InformerFor函数
	return f.factory.InformerFor(&appsv1.Deployment{}, f.defaultInformer)
}

func (f *deploymentInformer) Lister() v1.DeploymentLister {
	// v1.NewDeploymentLister 调用了k8s.io/client-go/listers/apps/v1/deployment.go
	// 这个函数主要是将shareindexinformer结构体的本地缓存传递进去了
	// 最后返回一个：type cache struct 让我们可以去这个里面查数据
	return v1.NewDeploymentLister(f.Informer().GetIndexer())
}
