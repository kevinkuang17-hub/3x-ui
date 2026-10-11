package service

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
)

const passkeyFlowTTL = 5 * time.Minute

type passkeyFlow struct {
	session webauthn.SessionData
	userID  int
	rpID    string
	expires time.Time
}

var (
	passkeyFlowsMu sync.Mutex
	passkeyFlows   = map[string]passkeyFlow{}
)

func storeFlow(f passkeyFlow) string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	id := hex.EncodeToString(b)
	passkeyFlowsMu.Lock()
	defer passkeyFlowsMu.Unlock()
	now := time.Now()
	for k, v := range passkeyFlows {
		if now.After(v.expires) {
			delete(passkeyFlows, k)
		}
	}
	f.expires = now.Add(passkeyFlowTTL)
	passkeyFlows[id] = f
	return id
}

// takeFlow returns and forgets a pending ceremony, so each one can finish once.
func takeFlow(id string) (passkeyFlow, bool) {
	passkeyFlowsMu.Lock()
	defer passkeyFlowsMu.Unlock()
	f, ok := passkeyFlows[id]
	delete(passkeyFlows, id)
	if !ok || time.Now().After(f.expires) {
		return passkeyFlow{}, false
	}
	return f, true
}

// passkeyUser adapts a panel user to the webauthn.User interface.
type passkeyUser struct {
	user  model.User
	creds []webauthn.Credential
}

func userHandle(id int) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(id))
	return b
}

func (u *passkeyUser) WebAuthnID() []byte                         { return userHandle(u.user.Id) }
func (u *passkeyUser) WebAuthnName() string                       { return u.user.Username }
func (u *passkeyUser) WebAuthnDisplayName() string                { return u.user.Username }
func (u *passkeyUser) WebAuthnCredentials() []webauthn.Credential { return u.creds }

func loadPasskeyUser(id int) (*passkeyUser, error) {
	db := database.GetDB()
	var u model.User
	if err := db.Where("id = ?", id).First(&u).Error; err != nil {
		return nil, common.NewError("user not found")
	}
	var rows []model.PasskeyCredential
	db.Where("user_id = ?", id).Find(&rows)
	pu := &passkeyUser{user: u}
	for _, r := range rows {
		var c webauthn.Credential
		if json.Unmarshal([]byte(r.Data), &c) == nil {
			pu.creds = append(pu.creds, c)
		}
	}
	return pu, nil
}

// PasskeyService implements passkey registration and password-less login. The relying party is
// the host the panel is opened on, so passkeys need a domain name (browsers refuse IPs).
type PasskeyService struct{}

// RelyingParty derives the WebAuthn relying party from the request.
func RelyingParty(r *http.Request, forwardedHTTPS bool) (*webauthn.WebAuthn, string, error) {
	host := r.Host
	hostname := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		hostname = h
	}
	if hostname == "" || net.ParseIP(strings.Trim(hostname, "[]")) != nil {
		return nil, "", common.NewError("passkeys need the panel to be opened by a domain name, not an IP address")
	}
	scheme := "http"
	if r.TLS != nil || forwardedHTTPS {
		scheme = "https"
	}
	if scheme != "https" && hostname != "localhost" {
		return nil, "", common.NewError("passkeys need the panel to be opened over https")
	}
	w, err := webauthn.New(&webauthn.Config{
		RPDisplayName: "3X-UI",
		RPID:          hostname,
		RPOrigins:     []string{scheme + "://" + host},
	})
	return w, hostname, err
}

func (s *PasskeyService) List(userID int) ([]model.PasskeyCredential, error) {
	var rows []model.PasskeyCredential
	err := database.GetDB().Where("user_id = ?", userID).Order("id asc").Find(&rows).Error
	return rows, err
}

func (s *PasskeyService) Delete(userID, id int) error {
	res := database.GetDB().Where("id = ? AND user_id = ?", id, userID).Delete(&model.PasskeyCredential{})
	if res.Error == nil && res.RowsAffected == 0 {
		return common.NewError("passkey not found")
	}
	return res.Error
}

func (s *PasskeyService) BeginRegistration(w *webauthn.WebAuthn, rpID string, userID int) (string, *protocol.CredentialCreation, error) {
	pu, err := loadPasskeyUser(userID)
	if err != nil {
		return "", nil, err
	}
	exclusions := make([]protocol.CredentialDescriptor, 0, len(pu.creds))
	for _, c := range pu.creds {
		exclusions = append(exclusions, c.Descriptor())
	}
	creation, session, err := w.BeginRegistration(pu,
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
		webauthn.WithExclusions(exclusions))
	if err != nil {
		return "", nil, err
	}
	return storeFlow(passkeyFlow{session: *session, userID: userID, rpID: rpID}), creation, nil
}

func (s *PasskeyService) FinishRegistration(w *webauthn.WebAuthn, rpID, flowID string, userID int, name string, r *http.Request) (*model.PasskeyCredential, error) {
	f, ok := takeFlow(flowID)
	if !ok || f.userID != userID || f.rpID != rpID {
		return nil, common.NewError("the passkey request expired; try again")
	}
	pu, err := loadPasskeyUser(userID)
	if err != nil {
		return nil, err
	}
	cred, err := w.FinishRegistration(pu, f.session, r)
	if err != nil {
		return nil, common.NewError("passkey was not accepted:", err)
	}
	data, err := json.Marshal(cred)
	if err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Passkey " + time.Now().Format("2006-01-02")
	}
	row := &model.PasskeyCredential{
		UserId: userID, CredentialId: base64.RawURLEncoding.EncodeToString(cred.ID), Data: string(data), Name: name,
	}
	if err := database.GetDB().Create(row).Error; err != nil {
		return nil, err
	}
	return row, nil
}

func (s *PasskeyService) BeginLogin(w *webauthn.WebAuthn, rpID string) (string, *protocol.CredentialAssertion, error) {
	assertion, session, err := w.BeginDiscoverableLogin()
	if err != nil {
		return "", nil, err
	}
	return storeFlow(passkeyFlow{session: *session, rpID: rpID}), assertion, nil
}

// FinishLogin verifies the assertion and returns the user it belongs to.
func (s *PasskeyService) FinishLogin(w *webauthn.WebAuthn, rpID, flowID string, r *http.Request) (*model.User, error) {
	f, ok := takeFlow(flowID)
	if !ok || f.userID != 0 || f.rpID != rpID {
		return nil, common.NewError("the passkey request expired; try again")
	}
	var found *passkeyUser
	handler := func(rawID, handle []byte) (webauthn.User, error) {
		if len(handle) != 8 {
			return nil, errors.New("unknown passkey")
		}
		pu, err := loadPasskeyUser(int(binary.BigEndian.Uint64(handle)))
		if err != nil {
			return nil, err
		}
		found = pu
		return pu, nil
	}
	cred, err := w.FinishDiscoverableLogin(handler, f.session, r)
	if err != nil || found == nil {
		return nil, common.NewError("passkey sign-in failed")
	}
	data, _ := json.Marshal(cred)
	database.GetDB().Model(&model.PasskeyCredential{}).
		Where("credential_id = ? AND user_id = ?", base64.RawURLEncoding.EncodeToString(cred.ID), found.user.Id).
		Updates(map[string]any{"data": string(data), "last_used_at": time.Now().Unix()})
	return &found.user, nil
}
