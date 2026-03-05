package processing

import (
	"context"
	"encoding/json"

	"github.com/interpt-co/flume/internal/models"
	"github.com/valyala/fastjson"
)

// parseWorker checks whether the message content is valid JSON.
// If it is, IsJson is set to true and JsonContent holds the raw bytes.
// The worker never returns an error: all content is valid, it may just not be JSON.
func parseWorker(_ context.Context, msg models.LogMessage) (models.LogMessage, error) {
	if fastjson.Validate(msg.Content) == nil {
		msg.IsJson = true
		msg.JsonContent = json.RawMessage(msg.Content)
	}
	return msg, nil
}
