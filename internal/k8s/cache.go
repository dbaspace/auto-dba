package k8s

import (
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/informers"
)

// InformerCache 按集群懒启动 SharedInformerFactory，空闲回收。
type InformerCache struct {
	mu      sync.Mutex
	entries map[string]*informerEntry
	idleFor time.Duration
}

type informerEntry struct {
	factory  informers.SharedInformerFactory
	lastUsed time.Time
	stopCh   chan struct{}
}

func NewInformerCache() *InformerCache {
	c := &InformerCache{
		entries: make(map[string]*informerEntry),
		idleFor: 30 * time.Minute,
	}
	go c.reaper()
	return c
}

func (c *InformerCache) reaper() {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for range t.C {
		c.mu.Lock()
		now := time.Now()
		for id, e := range c.entries {
			if now.Sub(e.lastUsed) > c.idleFor {
				close(e.stopCh)
				delete(c.entries, id)
			}
		}
		c.mu.Unlock()
	}
}

// Ensure 启动或续命 informer。
func (c *InformerCache) Ensure(cli *Client) informers.SharedInformerFactory {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[cli.ID]; ok {
		e.lastUsed = time.Now()
		return e.factory
	}
	f := informers.NewSharedInformerFactory(cli.Clientset, 5*time.Minute)
	stop := make(chan struct{})
	// 预热常用资源
	_ = f.Core().V1().Pods().Informer()
	_ = f.Core().V1().Nodes().Informer()
	_ = f.Apps().V1().Deployments().Informer()
	_ = f.Apps().V1().StatefulSets().Informer()
	_ = f.Core().V1().Services().Informer()
	f.Start(stop)
	f.WaitForCacheSync(stop)
	c.entries[cli.ID] = &informerEntry{factory: f, lastUsed: time.Now(), stopCh: stop}
	return f
}

// ListPodsFromCache 优先读缓存；尚未同步时返回 ok=false。
func (c *InformerCache) ListPodsFromCache(cli *Client, ns string) ([]metav1.Object, bool) {
	f := c.Ensure(cli)
	informer := f.Core().V1().Pods().Informer()
	if !informer.HasSynced() {
		return nil, false
	}
	var out []metav1.Object
	for _, obj := range informer.GetStore().List() {
		if m, ok := obj.(metav1.Object); ok {
			if ns == "" || ns == "*" || m.GetNamespace() == ns {
				out = append(out, m)
			}
		}
	}
	return out, true
}

// Stop 停止指定集群缓存。
func (c *InformerCache) Stop(clusterID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[clusterID]; ok {
		close(e.stopCh)
		delete(c.entries, clusterID)
	}
}

// CachedGVRs 提示哪些资源可走缓存。
func CachedGVRs() []schema.GroupVersionResource {
	return []schema.GroupVersionResource{
		{Group: "", Version: "v1", Resource: "pods"},
		{Group: "", Version: "v1", Resource: "nodes"},
		{Group: "", Version: "v1", Resource: "services"},
		{Group: "apps", Version: "v1", Resource: "deployments"},
		{Group: "apps", Version: "v1", Resource: "statefulsets"},
	}
}
