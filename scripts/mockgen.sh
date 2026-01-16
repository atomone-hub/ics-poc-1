#!/usr/bin/env bash

mockgen_cmd="mockgen"
$mockgen_cmd -source=modules/provider/types/expected_keepers.go -package testutil -destination modules/provider/testutil/expected_keepers_mocks.go
