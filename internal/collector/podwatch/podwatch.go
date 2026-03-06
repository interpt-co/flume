package podwatch

import (
	"context"
	"sync"

	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

// Watcher watches pods on the current node via the K8s API and maintains
// a cache of pod labels keyed by "namespace/pod".
type Watcher struct {
	mu     sync.RWMutex
	labels map[string]map[string]string // "namespace/pod" -> labels
}

// NewWatcher creates a new pod label cache.
func NewWatcher() *Watcher {
	return &Watcher{
		labels: make(map[string]map[string]string),
	}
}

// GetLabels returns the cached labels for a pod. Returns nil if not found.
func (w *Watcher) GetLabels(namespace, pod string) map[string]string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.labels[namespace+"/"+pod]
}

// SetLabels sets labels for a pod. Used for testing or manual enrichment.
// The map is copied to avoid races with the caller.
func (w *Watcher) SetLabels(namespace, pod string, labels map[string]string) {
	cp := make(map[string]string, len(labels))
	for k, v := range labels {
		cp[k] = v
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.labels[namespace+"/"+pod] = cp
}

// Start begins watching pods on the given node. It blocks until ctx is cancelled.
// nodeName should come from the K8s downward API (NODE_NAME env var).
func (w *Watcher) Start(ctx context.Context, nodeName string) error {
	config, err := rest.InClusterConfig()
	if err != nil {
		return err
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}

	return w.StartWithClient(ctx, clientset, nodeName)
}

// StartWithClient starts watching using a provided clientset (useful for testing).
func (w *Watcher) StartWithClient(ctx context.Context, clientset kubernetes.Interface, nodeName string) error {
	tweakFunc := informers.WithTweakListOptions(func(opts *metav1.ListOptions) {
		opts.FieldSelector = fields.OneTermEqualSelector("spec.nodeName", nodeName).String()
	})

	factory := informers.NewSharedInformerFactoryWithOptions(clientset, 0, tweakFunc)

	podInformer := factory.Core().V1().Pods().Informer()
	podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			pod, ok := obj.(*corev1.Pod)
			if !ok {
				return
			}
			w.updatePod(pod)
		},
		UpdateFunc: func(_, newObj interface{}) {
			pod, ok := newObj.(*corev1.Pod)
			if !ok {
				return
			}
			w.updatePod(pod)
		},
		DeleteFunc: func(obj interface{}) {
			pod, ok := obj.(*corev1.Pod)
			if !ok {
				tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
				if !ok {
					return
				}
				pod, ok = tombstone.Obj.(*corev1.Pod)
				if !ok {
					return
				}
			}
			w.removePod(pod)
		},
	})

	log.WithField("node", nodeName).Info("podwatch: starting informer")
	factory.Start(ctx.Done())
	synced := factory.WaitForCacheSync(ctx.Done())
	for _, ok := range synced {
		if !ok {
			log.Warn("podwatch: informer cache failed to sync")
			break
		}
	}

	<-ctx.Done()
	return nil
}

func (w *Watcher) updatePod(pod *corev1.Pod) {
	key := pod.Namespace + "/" + pod.Name
	labels := make(map[string]string, len(pod.Labels))
	for k, v := range pod.Labels {
		labels[k] = v
	}
	w.mu.Lock()
	w.labels[key] = labels
	w.mu.Unlock()
}

func (w *Watcher) removePod(pod *corev1.Pod) {
	key := pod.Namespace + "/" + pod.Name
	w.mu.Lock()
	delete(w.labels, key)
	w.mu.Unlock()
}
