package service

// RunningConfigCurrent reports whether the running core uses the config built from what is stored
// now, i.e. the last save was really applied (not refused or failed while the old core kept going).
func (s *XrayService) RunningConfigCurrent() bool {
	p := currentXrayProcess()
	if p == nil || !p.IsRunning() || xrayState.heldBackReason() != "" {
		return false
	}
	cfg, err := s.GetXrayConfig()
	if err != nil {
		return false
	}
	return p.GetConfig().Equals(cfg)
}
