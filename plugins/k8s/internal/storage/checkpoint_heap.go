package storage

import "container/heap"

type CheckpointHeap []*Checkpoint

func (ch CheckpointHeap) Len() int {
	return len(ch)
}

func (ch CheckpointHeap) Less(i, j int) bool {
	return ch[i].unixTime > ch[j].unixTime
}

func (ch CheckpointHeap) Swap(i, j int) {
	ch[i], ch[j] = ch[j], ch[i]
	ch[i].index = i
	ch[j].index = j
}

func (ch *CheckpointHeap) Push(x any) {
	n := len(*ch)
	item := x.(*Checkpoint)
	item.index = n
	*ch = append(*ch, item)
}

func (ch *CheckpointHeap) Pop() any {
	old := *ch
	n := len(old)
	item := old[n-1]
	old[n-1] = nil  // don't stop the GC from reclaiming the item eventually
	item.index = -1 // for safety
	*ch = old[0 : n-1]
	return item
}

func NewCheckpointHeap() CheckpointHeap {
	checkpointHeap := make(CheckpointHeap, 0)
	heap.Init(&checkpointHeap)
	return checkpointHeap
}
