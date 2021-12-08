package main

import (
	"context"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"log"
)

func main() {
	namespace := "ingress-nginx"
	name := "ingress-nginx-controller"

	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
	service, err := clientset.CoreV1().Services(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		panic(err.Error())
	}
	set := labels.Set(service.Spec.Selector)
	pods, err := clientset.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{LabelSelector: set.AsSelector().String()})
	if err != nil {
		panic(err.Error())
	}
	for _, pod := range pods.Items {
		log.Println(pod.GetName(), pod.Spec.NodeName, pod.Spec.Containers)
	}
}
