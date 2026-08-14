# S3 Storage Plugin

Adds S3 remote storage support for storing checkpoints in user S3 buckets.

Authentication is configured with `AWS.CredentialsMode`: `static` (the default),
`eksPodIdentity`, or `ambient`. Static mode requires both access-key fields. EKS
Pod Identity mode requires the container credential URI and authorization token
file injected by the EKS Pod Identity Agent. Ambient mode uses the AWS SDK's
default credential chain without EKS-specific validation.
