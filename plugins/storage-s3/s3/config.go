package s3

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	cedanaconfig "github.com/cedana/cedana/pkg/config"
)

const (
	CredentialsModeStatic         = "static"
	CredentialsModeEKSPodIdentity = "eksPodIdentity"
	CredentialsModeAmbient        = "ambient"
)

// LoadAWSConfig validates the selected authentication mode and creates the AWS
// configuration shared by S3 storage and its health check.
func LoadAWSConfig(ctx context.Context, settings cedanaconfig.AWS) (cfg aws.Config, err error) {
	options := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(settings.Region)}

	switch settings.CredentialsMode {
	case CredentialsModeStatic:
		if settings.AccessKeyID == "" || settings.SecretAccessKey == "" {
			return cfg, fmt.Errorf("AWS access_key_id and secret_access_key must both be set when credentials_mode is %q", CredentialsModeStatic)
		}
		options = append(options, awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			settings.AccessKeyID, settings.SecretAccessKey, "",
		)))
	case CredentialsModeEKSPodIdentity:
		if os.Getenv("AWS_CONTAINER_CREDENTIALS_FULL_URI") == "" {
			return cfg, fmt.Errorf("AWS_CONTAINER_CREDENTIALS_FULL_URI must be set when credentials_mode is %q", CredentialsModeEKSPodIdentity)
		}
		if os.Getenv("AWS_CONTAINER_AUTHORIZATION_TOKEN_FILE") == "" {
			return cfg, fmt.Errorf("AWS_CONTAINER_AUTHORIZATION_TOKEN_FILE must be set when credentials_mode is %q", CredentialsModeEKSPodIdentity)
		}
	case CredentialsModeAmbient:
		// The default chain supports IRSA, instance roles, environment variables,
		// shared config files, and other AWS-supported providers.
	default:
		return cfg, fmt.Errorf("unsupported AWS credentials_mode %q (expected static, eksPodIdentity, or ambient)", settings.CredentialsMode)
	}

	return awsconfig.LoadDefaultConfig(ctx, options...)
}
