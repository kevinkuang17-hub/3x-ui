package service

import (
	"context"
	"errors"
)

type CfOnlyStatus struct {
	Ports  []int `json:"ports"`
	Ranges int   `json:"ranges"`
}

var (
	errCfOnlyNoUfw            = errors.New("stub")
	errCfOnlyDefaultAllow     = errors.New("stub")
	errCfOnlyNotViaCloudflare = errors.New("stub")
	errCfOnlyRanges           = errors.New("stub")
)

var fetchCloudflareRanges = func(context.Context) ([]string, error) { return nil, nil }

func (s *ProtectService) EnableCfOnly(port int, viaCloudflare, panel bool) error { return nil }
func (s *ProtectService) DisableCfOnly(port int) error                           { return nil }
func (s *ProtectService) RefreshCfOnly() error                                   { return nil }
func (s *ProtectService) CfOnly() CfOnlyStatus                                   { return CfOnlyStatus{} }
