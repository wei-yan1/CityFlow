package svc

import (
	"context"

	paymentservice "cityflow/app/payment/application/payment"
	"cityflow/app/payment/repository"
	"cityflow/internal/common/pagination"
	"cityflow/internal/integration/rpcadapt"
)

// paymentTasks adapts Task's RPC snapshot to Payment's local projection. Task
// owns the aggregate and its schema; Payment only reads these facts.
type paymentTasks struct{ tasks rpcadapt.Tasks }

func (p paymentTasks) Get(ctx context.Context, id string) (*paymentservice.TaskView, error) {
	item, err := p.tasks.Snapshot(ctx, id)
	if err != nil {
		return nil, err
	}
	return taskView(item), nil
}

func (p paymentTasks) List(ctx context.Context, status string, page repository.Page) ([]*paymentservice.TaskView, error) {
	items, err := p.tasks.ListSnapshots(ctx, status, pagination.Page(page))
	if err != nil {
		return nil, err
	}
	out := make([]*paymentservice.TaskView, 0, len(items))
	for _, item := range items {
		out = append(out, taskView(item))
	}
	return out, nil
}

func taskView(item *rpcadapt.TaskSnapshot) *paymentservice.TaskView {
	if item == nil {
		return nil
	}
	return &paymentservice.TaskView{
		ID: item.ID, Title: item.Title, Type: item.Type, Status: item.Status,
		CreatedBy: item.CreatedBy, WorkerID: item.WorkerID, QuotedAmountFen: item.QuotedAmountFen,
	}
}
