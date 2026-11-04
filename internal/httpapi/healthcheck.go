package httpapi

import (
	"context"
)

type healthzBody struct {
	Status string `json:"status" example:"ok"`
}

type healthzOutput struct {
	Body healthzBody
}

// healthzHumaHandler — /healthz через Huma: попадает в спеку, валидация тривиальна.
func healthzHumaHandler(_ context.Context, _ *struct{}) (*healthzOutput, error) {
	out := &healthzOutput{}
	out.Body.Status = "ok"
	return out, nil
}
