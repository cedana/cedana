package propagator

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	sdk "github.com/cedana/cedana-go-sdk"
	"github.com/cedana/cedana-go-sdk/models"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/types"
)

func CheckConfig() types.Check {
	return func(ctx context.Context) []*daemon.HealthCheckComponent {
		components := []*daemon.HealthCheckComponent{}

		if config.Global.Connection.URL == "" {
			components = append(components, &daemon.HealthCheckComponent{
				Name:   "URL",
				Data:   "not set",
				Errors: []string{"Cedana URL is not set in the configuration"},
			})
		} else {
			components = append(components, &daemon.HealthCheckComponent{
				Name: "URL",
				Data: config.Global.Connection.URL,
			})
		}

		propagator := sdk.NewCedanaClient(config.Global.Connection.URL, config.Global.Connection.AuthToken).V2()
		_, err := propagator.User().Get(ctx, nil)
		if err == nil {
			components = append(components, &daemon.HealthCheckComponent{
				Name: "auth token",
				Data: "valid",
			})
		} else {
			var errMsg string
			switch v := err.(type) {
			case *models.HttpError:
				errMsg = *v.GetMessage()
			default:
				errMsg = err.Error()
			}
			components = append(components, &daemon.HealthCheckComponent{
				Name:   "auth token",
				Data:   "invalid",
				Errors: []string{errMsg},
			})
		}

		return components
	}
}
