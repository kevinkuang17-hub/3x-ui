package service

import (
	"errors"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
	"gorm.io/gorm"
)

// ClientRoutingView is what the client routing dialog shows: the client's own routing, and where
// the routing its subscription gets right now comes from.
type ClientRoutingView struct {
	Email string `json:"email" example:"alice"`
	Group string `json:"group" example:"vip"`
	// Own is true when the client has its own rules; OwnDns when it has its own DNS.
	Own        bool          `json:"own" example:"true"`
	ClashRules string        `json:"clashRules" example:""`
	RuleSets   []RuleSetPick `json:"ruleSets"`
	ExtraRules string        `json:"extraRules" example:"DOMAIN-SUFFIX,corp.example,DIRECT"`
	OwnDns     bool          `json:"ownDns" example:"false"`
	Dns        *DnsConfig    `json:"dns"`
	// Source is client, group or global: whose rules the subscription currently uses.
	Source string `json:"source" example:"client"`
}

var errClientNoSubID = errors.New("this client has no subscription ID, so it has no subscription to route")

func (s *ClientService) clientSubID(email string) (*model.ClientRecord, error) {
	rec, err := s.GetRecordByEmail(nil, strings.TrimSpace(email))
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(rec.SubID) == "" {
		return nil, common.NewError(errClientNoSubID.Error())
	}
	return rec, nil
}

func (s *ClientService) GetClientRouting(email string) (*ClientRoutingView, error) {
	rec, err := s.clientSubID(email)
	if err != nil {
		return nil, err
	}
	view := &ClientRoutingView{Email: rec.Email, Group: rec.Group, RuleSets: []RuleSetPick{}}
	var own model.ClientRouting
	err = database.GetDB().Where("sub_id = ?", rec.SubID).First(&own).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if err == nil {
		mine := routingFrom(own.ClashRules, own.ClashRuleSets, own.ClashExtraRules, own.ClashDns)
		view.Own, view.ClashRules, view.ExtraRules = mine.Override, mine.Template, mine.Choice.ExtraRules
		if mine.Choice.RuleSets != nil {
			view.RuleSets = mine.Choice.RuleSets
		}
		view.OwnDns, view.Dns = mine.Dns != nil, mine.Dns
	}
	view.Source = RoutingBySubID(rec.SubID).Source
	return view, nil
}

// SetClientRouting gives the client's subscription its own rules (choice non-nil) and DNS (dns
// non-nil); nil for either makes that part follow the group or global profile again.
func (s *ClientService) SetClientRouting(email, template string, choice *RoutingChoice, dns *DnsConfig) error {
	rec, err := s.clientSubID(email)
	if err != nil {
		return err
	}
	row := model.ClientRouting{SubId: rec.SubID}
	if choice != nil {
		canonical, remote, err := common.ParseRemoteRoutingURL(strings.TrimSpace(template))
		if err != nil {
			return common.NewError("Clash/Mihomo routing source", err.Error())
		}
		row.ClashRules = strings.TrimSpace(template)
		if remote {
			row.ClashRules = canonical
		}
		if row.ClashRuleSets, row.ClashExtraRules, err = NormalizeChoice(*choice); err != nil {
			return err
		}
		if row.ClashRules == "" && row.ClashRuleSets == "" && strings.TrimSpace(row.ClashExtraRules) == "" {
			return common.NewError("pick at least one rule set, rule or template, or follow the group")
		}
	}
	if dns != nil {
		if row.ClashDns, err = NormalizeDns(*dns); err != nil {
			return err
		}
	}
	db := database.GetDB()
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("sub_id = ?", rec.SubID).Delete(&model.ClientRouting{}).Error; err != nil {
			return err
		}
		if choice == nil && dns == nil {
			return nil
		}
		return tx.Create(&row).Error
	})
}
