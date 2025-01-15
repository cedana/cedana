#!/bin/bash

if [ -n "$AWS_ACCESS_KEY_ID" ]; then
    echo "AWS_ACCESS_KEY_ID=$AWS_ACCESS_KEY_ID" >> /etc/aws_conditional_env
fi
if [ -n "$AWS_SECRET_ACCESS_KEY" ]; then
    echo "AWS_SECRET_ACCESS_KEY=$AWS_SECRET_ACCESS_KEY" >> /etc/aws_conditional_env
fi
if [ -n "$AWS_DEFAULT_REGION" ]; then
    echo "AWS_DEFAULT_REGION=$AWS_DEFAULT_REGION" >> /etc/aws_conditional_env
fi
echo "AWS_CONFIG_FILE=$AWS_CONFIG_FILE" >> /etc/aws_conditional_env
echo "AWS_SHARED_CREDENTIALS_FILE=$AWS_SHARED_CREDENTIALS_FILE" >> /etc/aws_conditional_env

