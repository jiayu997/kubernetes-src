package restClient_Ingress_Controller_Demo

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/clientcmd"
)
import "k8s.io/client-go/rest"

func initRestConfig() *rest.Config {
	// init reset config
	config, err := clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
	if err != nil {
		panic(err)
	}
	return config
}

// 基于需求设置gvk
func initGVK(config *rest.Config, apiPath string, gV schema.GroupVersion, coder runtime.NegotiatedSerializer) {
	config.APIPath = apiPath
	config.GroupVersion = &gV
	// 设置编码器
	config.NegotiatedSerializer = coder
}

// return resetClient
func initRestClient(config *rest.Config) *rest.RESTClient {
	resetClient, err := rest.RESTClientFor(config)
	if err != nil {
		panic(err)
	}
	return resetClient
}
