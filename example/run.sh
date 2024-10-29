#!/usr/bin/env bash

export SURREALLOG_ENDPOINT='ws://localhost:8000/rpc'
export SURREALLOG_USER='logger'
export SURREALLOG_PASS='logger'
export SURREALLOG_NAMESPACE='test'

go run . $*
