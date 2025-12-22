#!/bin/bash

# This file contains helpers for our metrics and logging collector (SigNoz)

log_url_host() {
    if [ "$CEDANA_METRICS_ENABLED" != "true" ]; then
        echo -e "[Metrics collection is disabled, set CEDANA_METRICS_ENABLED=true to enable it]\n"
        return 0
    fi
    local cedana_url=$1
    local host_name
    host_name=$(hostname)
    if [ -z "$cedana_url" ]; then
        error_log "Cedana URL is required to get log URL"
        return 1
    fi
    if [ -z "$host_name" ]; then
        error_log "Host name is required to get log URL"
        return 1
    fi
    cedana_url="${cedana_url%/}"  # Remove trailing slash if any
    cedana_url=$(echo -n "$cedana_url" | sed -e 's/:/%253A/g' -e 's/\//%252F/g')
    echo -e "https://wzbl-jiit.us.signoz.cloud/logs/logs-explorer?compositeQuery=%257B%2522queryType%2522%253A%2522builder%2522%252C%2522builder%2522%253A%257B%2522queryData%2522%253A%255B%257B%2522dataSource%2522%253A%2522logs%2522%252C%2522queryName%2522%253A%2522A%2522%252C%2522aggregateOperator%2522%253A%2522count%2522%252C%2522aggregateAttribute%2522%253A%257B%2522id%2522%253A%2522----%2522%252C%2522dataType%2522%253A%2522%2522%252C%2522key%2522%253A%2522%2522%252C%2522type%2522%253A%2522%2522%257D%252C%2522timeAggregation%2522%253A%2522rate%2522%252C%2522spaceAggregation%2522%253A%2522sum%2522%252C%2522filter%2522%253A%257B%2522expression%2522%253A%2522cedana.service.url%2520%253D%2520%27${cedana_url}%27%2520AND%2520host.name%2520%253D%2520%27${host_name}%27%2522%257D%252C%2522aggregations%2522%253A%255B%257B%2522expression%2522%253A%2522count%28%29%2520%2522%257D%255D%252C%2522functions%2522%253A%255B%255D%252C%2522filters%2522%253A%257B%2522items%2522%253A%255B%255D%252C%2522op%2522%253A%2522AND%2522%257D%252C%2522expression%2522%253A%2522A%2522%252C%2522disabled%2522%253Afalse%252C%2522stepInterval%2522%253Anull%252C%2522having%2522%253A%257B%2522expression%2522%253A%2522%2522%257D%252C%2522limit%2522%253Anull%252C%2522orderBy%2522%253A%255B%255D%252C%2522groupBy%2522%253A%255B%255D%252C%2522legend%2522%253A%2522%2522%252C%2522reduceTo%2522%253A%2522avg%2522%252C%2522source%2522%253A%2522%2522%257D%255D%252C%2522queryFormulas%2522%253A%255B%255D%252C%2522queryTraceOperator%2522%253A%255B%255D%257D%252C%2522promql%2522%253A%255B%257B%2522name%2522%253A%2522A%2522%252C%2522query%2522%253A%2522%2522%252C%2522legend%2522%253A%2522%2522%252C%2522disabled%2522%253Afalse%257D%255D%252C%2522clickhouse_sql%2522%253A%255B%257B%2522name%2522%253A%2522A%2522%252C%2522legend%2522%253A%2522%2522%252C%2522disabled%2522%253Afalse%252C%2522query%2522%253A%2522%2522%257D%255D%252C%2522id%2522%253A%2522df0bb4e4-f15d-4ef9-aacc-04f18bf34f2b%2522%257D&options=%7B%22selectColumns%22%3A%5B%7B%22name%22%3A%22timestamp%22%2C%22signal%22%3A%22logs%22%2C%22fieldContext%22%3A%22log%22%2C%22fieldDataType%22%3A%22%22%2C%22isIndexed%22%3Afalse%7D%2C%7B%22name%22%3A%22service.name%22%2C%22signal%22%3A%22logs%22%2C%22fieldContext%22%3A%22resource%22%2C%22fieldDataType%22%3A%22string%22%7D%2C%7B%22name%22%3A%22body%22%2C%22signal%22%3A%22logs%22%2C%22fieldContext%22%3A%22log%22%2C%22fieldDataType%22%3A%22%22%2C%22isIndexed%22%3Afalse%7D%2C%7B%22name%22%3A%22error%22%2C%22signal%22%3A%22logs%22%2C%22fieldContext%22%3A%22attribute%22%2C%22fieldDataType%22%3A%22string%22%7D%5D%2C%22maxLines%22%3A1%2C%22format%22%3A%22raw%22%2C%22fontSize%22%3A%22small%22%7D"
}

log_url_cluster() {
    if [ "$CEDANA_METRICS_ENABLED" != "true" ]; then
        echo -e "[Metrics collection is disabled, set CEDANA_METRICS_ENABLED=true to enable it]\n"
        return 0
    fi
    local cedana_url=$1
    local cluster_id=$2
    if [ -z "$cedana_url" ]; then
        error_log "Cedana URL is required to get log URL"
        return 1
    fi
    if [ -z "$cluster_id" ]; then
        error_log "Cluster ID is required to get log URL"
        return 1
    fi
    cedana_url="${cedana_url%/}"  # Remove trailing slash if any
    cedana_url=$(echo -n "$cedana_url" | sed -e 's/:/%253A/g' -e 's/\//%252F/g')
    echo -e "https://wzbl-jiit.us.signoz.cloud/logs/logs-explorer?compositeQuery=%257B%2522queryType%2522%253A%2522builder%2522%252C%2522builder%2522%253A%257B%2522queryData%2522%253A%255B%257B%2522dataSource%2522%253A%2522logs%2522%252C%2522queryName%2522%253A%2522A%2522%252C%2522aggregateOperator%2522%253A%2522count%2522%252C%2522aggregateAttribute%2522%253A%257B%2522id%2522%253A%2522----%2522%252C%2522dataType%2522%253A%2522%2522%252C%2522key%2522%253A%2522%2522%252C%2522type%2522%253A%2522%2522%257D%252C%2522timeAggregation%2522%253A%2522rate%2522%252C%2522spaceAggregation%2522%253A%2522sum%2522%252C%2522filter%2522%253A%257B%2522expression%2522%253A%2522cedana.service.url%2520%253D%2520%27${cedana_url}%27%2520AND%2520cluster.id%2520%253D%2520%27${cluster_id}%27%2522%257D%252C%2522aggregations%2522%253A%255B%257B%2522expression%2522%253A%2522count%28%29%2520%2522%257D%255D%252C%2522functions%2522%253A%255B%255D%252C%2522filters%2522%253A%257B%2522items%2522%253A%255B%255D%252C%2522op%2522%253A%2522AND%2522%257D%252C%2522expression%2522%253A%2522A%2522%252C%2522disabled%2522%253Afalse%252C%2522stepInterval%2522%253Anull%252C%2522having%2522%253A%257B%2522expression%2522%253A%2522%2522%257D%252C%2522limit%2522%253Anull%252C%2522orderBy%2522%253A%255B%255D%252C%2522groupBy%2522%253A%255B%255D%252C%2522legend%2522%253A%2522%2522%252C%2522reduceTo%2522%253A%2522avg%2522%252C%2522source%2522%253A%2522%2522%257D%255D%252C%2522queryFormulas%2522%253A%255B%255D%252C%2522queryTraceOperator%2522%253A%255B%255D%257D%252C%2522promql%2522%253A%255B%257B%2522name%2522%253A%2522A%2522%252C%2522query%2522%253A%2522%2522%252C%2522legend%2522%253A%2522%2522%252C%2522disabled%2522%253Afalse%257D%255D%252C%2522clickhouse_sql%2522%253A%255B%257B%2522name%2522%253A%2522A%2522%252C%2522legend%2522%253A%2522%2522%252C%2522disabled%2522%253Afalse%252C%2522query%2522%253A%2522%2522%257D%255D%252C%2522id%2522%253A%2522991a0126-72a8-4dd3-9ef3-5c5402d88777%2522%257D&options=%7B%22selectColumns%22%3A%5B%7B%22name%22%3A%22timestamp%22%2C%22signal%22%3A%22logs%22%2C%22fieldContext%22%3A%22log%22%2C%22fieldDataType%22%3A%22%22%2C%22isIndexed%22%3Afalse%7D%2C%7B%22name%22%3A%22service.name%22%2C%22signal%22%3A%22logs%22%2C%22fieldContext%22%3A%22resource%22%2C%22fieldDataType%22%3A%22string%22%7D%2C%7B%22name%22%3A%22body%22%2C%22signal%22%3A%22logs%22%2C%22fieldContext%22%3A%22log%22%2C%22fieldDataType%22%3A%22%22%2C%22isIndexed%22%3Afalse%7D%2C%7B%22name%22%3A%22error%22%2C%22signal%22%3A%22logs%22%2C%22fieldContext%22%3A%22attribute%22%2C%22fieldDataType%22%3A%22string%22%7D%5D%2C%22maxLines%22%3A1%2C%22format%22%3A%22raw%22%2C%22fontSize%22%3A%22small%22%7D"
}
