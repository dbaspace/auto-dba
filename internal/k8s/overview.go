package k8s

import (
	"context"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// Overview 集群概览。
type Overview struct {
	Version       string         `json:"version"`
	NodeReady     int            `json:"node_ready"`
	NodeTotal     int            `json:"node_total"`
	PodPhases     map[string]int `json:"pod_phases"`
	AbnormalPods  []PodBrief     `json:"abnormal_pods"`
	RecentEvents  []EventBrief   `json:"recent_events"`
	CPURequests   string         `json:"cpu_requests,omitempty"`
	CPUAlloc      string         `json:"cpu_allocatable,omitempty"`
	MemRequests   string         `json:"mem_requests,omitempty"`
	MemAlloc      string         `json:"mem_allocatable,omitempty"`
}

type PodBrief struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Phase     string `json:"phase"`
	Reason    string `json:"reason,omitempty"`
	Node      string `json:"node,omitempty"`
}

type EventBrief struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Reason    string `json:"reason"`
	Message   string `json:"message"`
	Count     int32  `json:"count"`
	LastTime  string `json:"last_time,omitempty"`
}

// GetOverview 汇总集群概览。
func (c *Client) GetOverview(ctx context.Context, nsAllow []string) (*Overview, error) {
	ov := &Overview{PodPhases: map[string]int{}}
	if info, err := c.Discovery.ServerVersion(); err == nil {
		ov.Version = info.GitVersion
	}
	nodes, err := c.Clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{Limit: 500})
	if err == nil {
		ov.NodeTotal = len(nodes.Items)
		var cpuAlloc, memAlloc resource.Quantity
		for _, n := range nodes.Items {
			for _, cond := range n.Status.Conditions {
				if cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue {
					ov.NodeReady++
					break
				}
			}
			if q, ok := n.Status.Allocatable[corev1.ResourceCPU]; ok {
				cpuAlloc.Add(q)
			}
			if q, ok := n.Status.Allocatable[corev1.ResourceMemory]; ok {
				memAlloc.Add(q)
			}
		}
		ov.CPUAlloc = cpuAlloc.String()
		ov.MemAlloc = memAlloc.String()
	}

	allowSet := map[string]bool{}
	for _, n := range nsAllow {
		if n != "" && n != "*" {
			allowSet[n] = true
		}
	}
	pods, err := c.Clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{Limit: 2000})
	if err == nil {
		var cpuReq, memReq resource.Quantity
		for _, p := range pods.Items {
			if len(allowSet) > 0 && !allowSet[p.Namespace] {
				continue
			}
			phase := string(p.Status.Phase)
			ov.PodPhases[phase]++
			abnormal := phase != "Running" && phase != "Succeeded"
			reason := ""
			for _, cs := range p.Status.ContainerStatuses {
				if cs.State.Waiting != nil {
					reason = cs.State.Waiting.Reason
					if reason == "CrashLoopBackOff" || reason == "ImagePullBackOff" || reason == "ErrImagePull" {
						abnormal = true
					}
				}
				if cs.LastTerminationState.Terminated != nil && cs.LastTerminationState.Terminated.Reason == "OOMKilled" {
					abnormal = true
					reason = "OOMKilled"
				}
			}
			if abnormal {
				ov.AbnormalPods = append(ov.AbnormalPods, PodBrief{
					Namespace: p.Namespace, Name: p.Name, Phase: phase, Reason: reason, Node: p.Spec.NodeName,
				})
			}
			for _, ctn := range p.Spec.Containers {
				if q := ctn.Resources.Requests.Cpu(); q != nil {
					cpuReq.Add(*q)
				}
				if q := ctn.Resources.Requests.Memory(); q != nil {
					memReq.Add(*q)
				}
			}
		}
		ov.CPURequests = cpuReq.String()
		ov.MemRequests = memReq.String()
		sort.Slice(ov.AbnormalPods, func(i, j int) bool {
			return ov.AbnormalPods[i].Name < ov.AbnormalPods[j].Name
		})
		if len(ov.AbnormalPods) > 20 {
			ov.AbnormalPods = ov.AbnormalPods[:20]
		}
	}

	evts, err := c.Clientset.CoreV1().Events("").List(ctx, metav1.ListOptions{Limit: 100})
	if err == nil {
		sort.Slice(evts.Items, func(i, j int) bool {
			return eventTime(evts.Items[i]).After(eventTime(evts.Items[j]).Time)
		})
		for _, e := range evts.Items {
			if e.Type != "Warning" {
				continue
			}
			if len(allowSet) > 0 && e.Namespace != "" && !allowSet[e.Namespace] {
				continue
			}
			ov.RecentEvents = append(ov.RecentEvents, EventBrief{
				Namespace: e.Namespace,
				Name:      e.Name,
				Type:      e.Type,
				Reason:    e.Reason,
				Message:   truncate(e.Message, 200),
				Count:     e.Count,
				LastTime:  eventTime(e).UTC().Format("2006-01-02T15:04:05Z"),
			})
			if len(ov.RecentEvents) >= 15 {
				break
			}
		}
	}
	return ov, nil
}

func eventTime(e corev1.Event) metav1.Time {
	if !e.LastTimestamp.IsZero() {
		return e.LastTimestamp
	}
	if e.EventTime.IsZero() {
		return e.FirstTimestamp
	}
	return metav1.Time{Time: e.EventTime.Time}
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
