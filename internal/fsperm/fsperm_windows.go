//go:build windows

package fsperm

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

const POSIX = false

func currentUser() (*windows.SID, error) {
	u, e := windows.GetCurrentProcessToken().GetTokenUser()
	if e != nil {
		return nil, e
	}
	return u.User.Sid.Copy()
}

func privateFile(f *os.File, _ os.FileInfo) error {
	sd, e := windows.GetSecurityInfo(windows.Handle(f.Fd()), windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if e != nil {
		return e
	}
	dacl, _, e := sd.DACL()
	if e != nil || dacl == nil {
		return ErrNotPrivate
	}
	owner, _, e := sd.Owner()
	if e != nil {
		return e
	}
	user, e := currentUser()
	if e != nil {
		return e
	}
	for i := uint32(0); i < uint32(dacl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if e = windows.GetAce(dacl, i, &ace); e != nil {
			return e
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 {
			continue
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if sid.Equals(user) || sid.Equals(owner) || sid.IsWellKnown(windows.WinLocalSystemSid) || sid.IsWellKnown(windows.WinBuiltinAdministratorsSid) {
			continue
		}
		return ErrNotPrivate
	}
	return nil
}

func privateDir(os.FileInfo) error { return nil }

func Restrict(path string) error {
	user, e := currentUser()
	if e != nil {
		return e
	}
	acl, e := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.GENERIC_ALL,
		AccessMode:        windows.GRANT_ACCESS,
		Inheritance:       windows.NO_INHERITANCE,
		Trustee:           windows.TRUSTEE{TrusteeForm: windows.TRUSTEE_IS_SID, TrusteeType: windows.TRUSTEE_IS_USER, TrusteeValue: windows.TrusteeValueFromSID(user)},
	}}, nil)
	if e != nil {
		return e
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil)
}

func hint(path string) string {
	return fmt.Sprintf(`icacls "%s" /inheritance:r /grant:r "%%USERNAME%%:F"`, path)
}
