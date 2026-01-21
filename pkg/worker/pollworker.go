package worker

import (
	"context"
	"log"
	"sync"
	"time"

	"predictive-analysis-engine/pkg/clients/graph"
	"predictive-analysis-engine/pkg/clients/telemetry"
	"predictive-analysis-engine/pkg/config"
)

type PollWorker struct {
	graphClient     *graph.Client
	telemetryClient *telemetry.TelemetryClient
	cfg             *config.Config
	stopCh          chan struct{}
	wg              sync.WaitGroup
	running         bool
	runLock         sync.Mutex
}

func NewPollWorker(cfg *config.Config, gClient *graph.Client, tClient *telemetry.TelemetryClient) *PollWorker {
	return &PollWorker{
		graphClient:     gClient,
		telemetryClient: tClient,
		cfg:             cfg,
		stopCh:          make(chan struct{}),
	}
}

func (w *PollWorker) Start() {
	if !w.cfg.TelemetryWorker.Enabled {
		log.Println("[PollWorker] Disabled (TELEMETRY_WORKER_ENABLED=false)")
		return
	}

	w.runLock.Lock()
	if w.running {
		w.runLock.Unlock()
		log.Println("[PollWorker] Already running")
		return
	}
	w.running = true
	w.runLock.Unlock()

	log.Printf("[PollWorker] Starting with %dms interval\n", w.cfg.TelemetryWorker.PollIntervalMs)

	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.poll()

		ticker := time.NewTicker(time.Duration(w.cfg.TelemetryWorker.PollIntervalMs) * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-w.stopCh:
				return
			case <-ticker.C:
				w.poll()
			}
		}
	}()
}

func (w *PollWorker) Stop() {
	w.runLock.Lock()
	if !w.running {
		w.runLock.Unlock()
		return
	}
	w.running = false
	w.runLock.Unlock()

	log.Println("[PollWorker] Stopping...")
	close(w.stopCh)
	w.wg.Wait()

	log.Println("[PollWorker] Stopped")
}

func (w *PollWorker) poll() {
	log.Println("[PollWorker] Polling Graph Engine...")
	ctx := context.Background()

	var servicePoints []telemetry.ServicePoint
	var edgePoints []telemetry.EdgePoint

	snapshot, err := w.graphClient.GetMetricsSnapshot(ctx)
	if err != nil {
		log.Printf("[PollWorker] Snapshot fetch failed: %v\n", err)
	} else if snapshot != nil {

		for _, svc := range snapshot.Services {
			hasTraffic := svc.RPS > 0

			var rps, errRate, p95, p50, p99, avail *float64

			r := svc.RPS
			rps = &r

			if hasTraffic {
				e := svc.ErrorRate
				errRate = &e
				p95Val := svc.P95
				p95 = &p95Val
			}

			servicePoints = append(servicePoints, telemetry.ServicePoint{
				Name:         svc.Name,
				Namespace:    svc.Namespace,
				RequestRate:  rps,
				ErrorRate:    errRate,
				P95:          p95,
				P50:          p50,
				P99:          p99,
				Availability: avail,
			})
		}

		for _, edge := range snapshot.Edges {
			hasTraffic := edge.RPS > 0
			var rps, errRate, p95, p50, p99 *float64

			r := edge.RPS
			rps = &r

			if hasTraffic {
				e := edge.ErrorRate
				errRate = &e
				p := edge.P95
				p95 = &p
			}

			edgePoints = append(edgePoints, telemetry.EdgePoint{
				From:        edge.From,
				To:          edge.To,
				Namespace:   edge.Namespace,
				RequestRate: rps,
				ErrorRate:   errRate,
				P95:         p95,
				P50:         p50,
				P99:         p99,
			})
		}
	}

	var nodePoints []telemetry.PkgNodePoint
	var podPoints []telemetry.PkgPodPoint

	services, err := w.graphClient.GetServices(ctx)
	if err != nil {
		log.Printf("[PollWorker] Infra fetch failed: %v\n", err)
	} else {

		type uniqueNode struct {
			NodePlacement graph.NodePlacement
			Pods          []graph.PodInfo
		}
		uniqueNodes := make(map[string]*uniqueNode)

		for _, svc := range services {

			for _, node := range svc.Placement.Nodes {
				if node.Node == "" {
					continue
				}

				if _, exists := uniqueNodes[node.Node]; !exists {

					podsCopy := make([]graph.PodInfo, len(node.Pods))
					copy(podsCopy, node.Pods)
					uniqueNodes[node.Node] = &uniqueNode{
						NodePlacement: node,
						Pods:          podsCopy,
					}
				} else {

					existing := uniqueNodes[node.Node]
					for _, newPod := range node.Pods {
						found := false
						for _, exPod := range existing.Pods {
							if exPod.Name == newPod.Name {
								found = true
								break
							}
						}
						if !found {
							existing.Pods = append(existing.Pods, newPod)
						}
					}
				}
			}
		}

		for _, u := range uniqueNodes {

			cpuUse := u.NodePlacement.Resources.CPU.UsagePercent
			cpuCores := float64(u.NodePlacement.Resources.CPU.Cores)
			ramUsed := float64(u.NodePlacement.Resources.RAM.UsedMB)
			ramTotal := float64(u.NodePlacement.Resources.RAM.TotalMB)
			podCount := float64(len(u.Pods))

			nodePoints = append(nodePoints, telemetry.PkgNodePoint{
				Name:            u.NodePlacement.Node,
				CPUUsagePercent: &cpuUse,
				CPUTotalCores:   &cpuCores,
				RAMUsedMB:       &ramUsed,
				RAMTotalMB:      &ramTotal,
				PodCount:        &podCount,
			})

			for _, pod := range u.Pods {
				ram := pod.RAMUsedMB
				cpuPct := pod.CPUUsagePercent

				podPoints = append(podPoints, telemetry.PkgPodPoint{
					Name:            pod.Name,
					NodeName:        u.NodePlacement.Node,
					RAMUsedMB:       &ram,
					CPUUsagePercent: &cpuPct,
				})
			}
		}
	}

	if len(servicePoints) > 0 {
		if err := w.telemetryClient.WriteServiceMetrics(ctx, servicePoints); err != nil {
			log.Printf("[PollWorker] Write service metrics failed: %v", err)
		}
	}

	if len(edgePoints) > 0 {
		if err := w.telemetryClient.WriteEdgeMetrics(ctx, edgePoints); err != nil {
			log.Printf("[PollWorker] Write edge metrics failed: %v", err)
		}
	}

	if len(nodePoints) > 0 {

		if err := w.telemetryClient.WriteInfrastructureMetrics(ctx, nodePoints, podPoints); err != nil {
			log.Printf("[PollWorker] Write infra metrics failed: %v", err)
		}
	}

	log.Printf("[PollWorker] Poll complete: %d services, %d edges, %d nodes\n", len(servicePoints), len(edgePoints), len(nodePoints))
}
