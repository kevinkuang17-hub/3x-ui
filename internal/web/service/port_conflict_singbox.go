package service

import (
	"fmt"

	"gorm.io/gorm"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/singbox"
)

// Every local AnyTLS/Naive inbound owns an Xray SOCKS5 relay on 127.0.0.1 at a port derived from
// its id; no row holds that port, so these checks keep other inbounds off it and vice versa.
func checkSingboxSocksConflict(db *gorm.DB, inbound *model.Inbound, ignoreId int, newBits transportBits) (*portConflictDetail, error) {
	if newBits&transportTCP == 0 {
		return nil, nil
	}
	var candidates []*model.Inbound
	q := db.Model(model.Inbound{}).Where("protocol IN ? AND node_id IS NULL", singboxProtocols)
	if ignoreId > 0 {
		q = q.Where("id != ?", ignoreId)
	}
	if err := q.Find(&candidates).Error; err != nil {
		return nil, err
	}
	for _, c := range candidates {
		if singbox.RelayPortForInbound(c.Id) == inbound.Port {
			return &portConflictDetail{
				InboundID: c.Id, Remark: c.Remark, Tag: c.Tag, Listen: "127.0.0.1",
				Port: inbound.Port, Relay: true, Transports: transportTCP,
			}, nil
		}
	}
	return nil, nil
}

func singboxSelfConflict(inbound *model.Inbound, id int) string {
	if id <= 0 || inbound.NodeID != nil || !listenOverlaps(loopbackBind, inboundBindAddr(inbound)) {
		return ""
	}
	if relayPort := singbox.RelayPortForInbound(id); inbound.Port == relayPort {
		return fmt.Sprintf("port %d is inbound #%d's own SOCKS5 relay port on 127.0.0.1; choose a different port", relayPort, id)
	}
	return ""
}

// checkSingboxRelayCollision reports another sidecar inbound or a loopback-reachable inbound
// already holding the relay port that id derives.
func checkSingboxRelayCollision(db *gorm.DB, id int) (*portConflictDetail, error) {
	relayPort := singbox.RelayPortForInbound(id)
	var candidates []*model.Inbound
	if err := db.Model(model.Inbound{}).Where("node_id IS NULL AND id != ?", id).Find(&candidates).Error; err != nil {
		return nil, err
	}
	for _, c := range candidates {
		sidecarClash := isSingboxProtocol(c.Protocol) && singbox.RelayPortForInbound(c.Id) == relayPort
		portClash := c.Port == relayPort && listenOverlaps(loopbackBind, inboundBindAddr(c)) &&
			inboundTransports(c.Protocol, c.StreamSettings, c.Settings)&transportTCP != 0
		if sidecarClash || portClash {
			return &portConflictDetail{
				InboundID: c.Id, Remark: c.Remark, Tag: c.Tag, Listen: "127.0.0.1",
				Port: relayPort, Relay: true, Transports: transportTCP,
			}, nil
		}
	}
	return nil, nil
}
