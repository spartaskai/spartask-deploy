package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// installStateFile marks an installation that has not finished. spartaskctl install writes it
// together with .env, records each completed step in it and removes it at the end; while it
// exists, running install again continues the installation instead of refusing the directory.
const installStateFile = "install-state.json"

// installState is what an unfinished installation still has to do. The administrator's password
// is never stored: a continued installation asks for it again.
type installState struct {
	Activation *pendingActivation `json:"activation,omitempty"` // code not yet exchanged for the license
	Company    *companyState      `json:"company,omitempty"`    // the activated installation's first company
	BackupJob  bool               `json:"backup_job"`
}

// pendingActivation is an activation code and what the installation's first company needs from
// the operator. The license service accepts the code again with the same installation key (saved
// in .env), so exchanging it again after an interruption is safe.
type pendingActivation struct {
	Code          string `json:"code"`
	LicenseServer string `json:"license_server"`
	CompanyID     string `json:"company_id,omitempty"`
	AdminEmail    string `json:"admin_email"`
	AdminName     string `json:"admin_name"`
}

// companyState is the activated installation's first company, created once the stack runs.
type companyState struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Address          string `json:"address"`
	AdminEmail       string `json:"admin_email"`
	AdminName        string `json:"admin_name"`
	License          string `json:"license"`
	LicenseInstalled bool   `json:"license_installed"`
	Created          bool   `json:"created"`
}

func newInstallState(o installOptions) *installState {
	state := &installState{BackupJob: o.backupCron}
	if o.activation != "" {
		state.Activation = &pendingActivation{Code: o.activation, LicenseServer: o.licenseServer, CompanyID: o.companyID,
			AdminEmail: o.admin.email, AdminName: o.admin.name}
	}
	return state
}

// readInstallState reads the state of an unfinished installation; os.ErrNotExist means the
// installation in s is complete (or was never started).
func readInstallState(s stack) (*installState, error) {
	data, err := os.ReadFile(s.path(installStateFile))
	if err != nil {
		return nil, err
	}
	var state installState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("read %s: %w (remove it only if the installation is complete)", installStateFile, err)
	}
	return &state, nil
}

func (st *installState) save(s stack) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", installStateFile, err)
	}
	return writeSecretFile(s.path(installStateFile), append(data, '\n'))
}

// removeInstallState marks the installation in s as complete.
func removeInstallState(s stack) error {
	if err := os.Remove(s.path(installStateFile)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", installStateFile, err)
	}
	return nil
}
