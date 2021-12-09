package main

import (
	"context"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/robbilie/service-call-multiplexer/logger"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

var (
	requestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total number of http requests handled",
	}, []string{"status"})
	validationTime = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "nginx_subrequest_auth_jwt_token_validation_time_seconds",
		Help:    "Number of seconds spent validating token",
		Buckets: prometheus.ExponentialBuckets(100*time.Nanosecond.Seconds(), 3, 6),
	})
)

func init() {
	requestsTotal.WithLabelValues("200")
	requestsTotal.WithLabelValues("401")
	requestsTotal.WithLabelValues("405")
	requestsTotal.WithLabelValues("500")

	prometheus.MustRegister(
		requestsTotal,
		validationTime,
	)
}

func main() {
	loggerInstance := logger.NewLogger(getenv("LOG_LEVEL", "info")) // "debug", "info", "warn", "error", "fatal"
	server, err := newServer(
		loggerInstance,
		getenv("SERVICE_NAMESPACE", "ingress-nginx"),
		getenv("SERVICE_NAME", "ingress-nginx-controller"),
		getenv("SERVICE_PORT", "80"),
		getenv("SERVICE_PROTOCOL", "http"),
	)
	if err != nil {
		loggerInstance.Fatalw("Couldn't initialize server", "err", err)
	}

	http.HandleFunc("/", server.handleRequest)

	bindAddr := ":" + getenv("PORT", "8080")

	loggerInstance.Infow("Starting server", "addr", bindAddr)
	err = http.ListenAndServe(bindAddr, nil)

	if err != nil {
		loggerInstance.Fatalw("Error running server", "err", err)
	}
}

type server struct {
	Namespace     string
	LabelSelector string
	Port          string
	Protocol      string
	ClientSet     *kubernetes.Clientset
	HttpClient    http.Client
	Logger        logger.Logger
}

func newServer(logger logger.Logger, serviceNamespace string, serviceName string, servicePort string, serviceProtocol string) (*server, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}
	clientSet, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
	service, err := clientSet.CoreV1().Services(serviceNamespace).Get(context.TODO(), serviceName, metav1.GetOptions{})
	if err != nil {
		panic(err.Error())
	}
	set := labels.Set(service.Spec.Selector)
	pods, err := clientSet.CoreV1().Pods(serviceNamespace).List(context.TODO(), metav1.ListOptions{LabelSelector: set.AsSelector().String()})
	if err != nil {
		panic(err.Error())
	}
	for _, pod := range pods.Items {
		log.Println(pod.GetName(), pod.Spec.NodeName, pod.Spec.Containers)
	}
	client := http.Client{
		Timeout: time.Duration(10 * time.Second),
	}
	return &server{
		Namespace:     serviceNamespace,
		LabelSelector: set.AsSelector().String(),
		Port:          servicePort,
		Protocol:      serviceProtocol,
		ClientSet:     clientSet,
		HttpClient:    client,
		Logger:        logger,
	}, nil
}

func getenv(key, fallback string) string {
	value := os.Getenv(key)
	if len(value) == 0 {
		return fallback
	}
	return value
}

func (s *server) handleRequest(rw http.ResponseWriter, r *http.Request) {
	s.Logger.Debugw("Handled validation request", "method", r.Method, "url", r.URL, "status", http.StatusNoContent, "method", r.Method, "userAgent", r.UserAgent())

	pods, err := s.ClientSet.CoreV1().Pods(s.Namespace).List(context.TODO(), metav1.ListOptions{LabelSelector: s.LabelSelector})
	if err != nil {
		panic(err.Error())
	}

	ch := make(chan int)
	var wg sync.WaitGroup
	for _, pod := range pods.Items {
		wg.Add(1)
		go s.makeCall(pod, r, ch, &wg)
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	for statusCode := range ch {
		if statusCode < 100 || statusCode >= 300 {
			panic(err.Error())
		}
	}
	rw.WriteHeader(http.StatusNoContent)
}

func (s *server) makeCall(pod v1.Pod, r *http.Request, ch chan<- int, wg *sync.WaitGroup) {
	defer wg.Done()
	log.Println(pod.GetName(), pod.Spec.NodeName, pod.Spec.Containers)
	request, err := http.NewRequest(r.Method, s.Protocol+"://"+pod.Status.PodIP+r.URL.Path, nil)
	if err != nil {
		panic(err.Error())
	}
	response, err := s.HttpClient.Do(request)
	if err != nil {
		panic(err.Error())
	}
	ch <- response.StatusCode
}
