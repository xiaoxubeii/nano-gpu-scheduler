package dealer

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	v1 "k8s.io/api/core/v1"

	"github.com/nano-gpu/nano-gpu-scheduler/pkg/utils"
)

// GPUResource ─┬─> GPUs
//              └─> Demand ─> Plan

type Plan struct {
	Demand     Demand
	GPUIndexes []int
	Score      int
}

func NewPlanFromPod(pod *v1.Pod) (*Plan, error) {
	if !utils.IsAssumed(pod) {
		return nil, fmt.Errorf("pod %s/%s is not assumed", pod.Namespace, pod.Name)
	}
	plan := &Plan{
		Demand:     make(Demand, len(pod.Spec.Containers)),
		GPUIndexes: make([]int, len(pod.Spec.Containers)),
		Score:      0,
	}
	for i, c := range pod.Spec.Containers {
		plan.Demand[i] = GPUResource{
			Core:   utils.GetGPUCoreFromContainer(&c),
			Memory: utils.GetGPUMemoryFromContainer(&c),
		}
		idx, err := utils.GetContainerAssignIndex(pod, c.Name)
		if err != nil {
			idx = 0
		}
		plan.GPUIndexes[i] = idx
	}

	return plan, nil
}

type Demand []GPUResource

func NewDemandFromPod(pod *v1.Pod) Demand {
	ans := make(Demand, len(pod.Spec.Containers))
	for i, container := range pod.Spec.Containers {
		ans[i] = GPUResource{
			Core:   utils.GetGPUCoreFromContainer(&container),
			Memory: utils.GetGPUMemoryFromContainer(&container),
		}
	}
	return ans
}

func (d *Demand) String() string {
	buffer := bytes.Buffer{}
	for _, resource := range *d {
		buffer.Write([]byte(resource.String()))
	}
	return buffer.String()
}

func (d *Demand) Hash() string {
	to := func(bs [32]byte) []byte { return bs[0:32] }
	return hex.EncodeToString(to(sha256.Sum256([]byte(d.String()))))[0:8]
}

type GPUs []*GPUResource

func (g GPUs) Choose(demand Demand, rater Rater) (ans *Plan, err error) {
	var (
		dfs     func(i int)
		indexes = make([]int, len(demand))
	)
	dfs = func(position int) {
		if position == len(demand) {
			curr := &Plan{
				Demand:     demand,
				GPUIndexes: utils.CloneInts(indexes),
			}
			curr.Score = rater.Rate(g, curr)
			if ans != nil && ans.Score > curr.Score {
				return
			}
			ans = curr
			return
		}
		for i, gpu := range g {
			if !gpu.CanAllocate(demand[position]) {
				continue
			}
			gpu.Sub(demand[position])
			indexes[position] = i
			dfs(position + 1)
			gpu.Add(demand[position])
		}
	}
	dfs(0)
	if ans == nil {
		err = fmt.Errorf("allocate %s on %s failed", demand, g)
	}
	return
}

func (g GPUs) Allocate(plan *Plan) error {
	for i := 0; i < len(plan.GPUIndexes); i++ {
		if !g[plan.GPUIndexes[i]].CanAllocate(plan.Demand[i]) {
			return fmt.Errorf("can't apply plan %v on %s", plan, g)
		}
		g[plan.GPUIndexes[i]].Sub(plan.Demand[i])
	}
	return nil
}

func (g GPUs) Release(plan *Plan) error {
	for i := 0; i < len(plan.Demand); i++ {
		if plan.GPUIndexes[i] >= len(g) {
			return fmt.Errorf("allocate plan's GPU index %d bigger then GPU resource", plan.GPUIndexes[i])
		}
		g[plan.GPUIndexes[i]].Add(plan.Demand[i])
	}
	return nil
}

func (g GPUs) String() string {
	buffer := bytes.Buffer{}
	for _, resource := range g {
		buffer.Write([]byte(resource.String()))
	}
	return buffer.String()
}

type GPUResource struct {
	Core   int
	Memory int
}

func (g GPUResource) String() string {
	return fmt.Sprintf("(%d, %d)", g.Core, g.Memory)
}

func (g *GPUResource) Add(resource GPUResource) {
	g.Memory += resource.Memory
	g.Core += resource.Core
}

func (g *GPUResource) Sub(resource GPUResource) {
	g.Memory -= resource.Memory
	g.Core -= resource.Core
}

func (g *GPUResource) CanAllocate(resource GPUResource) bool {
	return g.Core >= resource.Core && g.Memory >= resource.Memory
}
