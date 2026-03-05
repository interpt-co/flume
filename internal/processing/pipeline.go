package processing

import (
	"github.com/interpt-co/flume/internal/models"
	"github.com/paulofilip3/pipeline"
)

// NewPipeline creates a pipeline with parse and enrich stages.
// Both stages use Concurrency: 1 to preserve message ordering.
func NewPipeline() (*pipeline.Pipeline[models.LogMessage], error) {
	return pipeline.New(
		pipeline.Stage[models.LogMessage]{
			Name:        "parse",
			Worker:      parseWorker,
			Concurrency: 1,
		},
		pipeline.Stage[models.LogMessage]{
			Name:        "enrich",
			Worker:      enrichWorker,
			Concurrency: 1,
		},
	)
}
