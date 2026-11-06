package main

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/madkoding/kitties/internal/vault"
	"github.com/madkoding/kitties/internal/whatsapp"
	"github.com/madkoding/kitties/internal/whatsapp/wameow"
)

// whatsappBridge is the WhatsApp link of the gateway (off until Settings turns it on). Its
// session is sealed with the vault's key: without a vault it stays off and Settings says why.
func (a *app) whatsappBridge(warn io.Writer, logf func(string, ...any)) *whatsapp.Bridge {
	dataDir := a.cfg.DataDirOr()
	opt := whatsapp.Options{
		Dir: filepath.Join(dataDir, "whatsapp"), Chats: a.chats, Kitty: a.fleet.Get, Registry: a.fleet.Connectors(),
		QRImage: wameow.QRImage, Log: logf,
	}
	if a.vault != nil {
		if box, err := vault.NewBox(dataDir); err == nil {
			opt.Box, opt.NewClient = box, wameow.New
		}
	}
	b, err := whatsapp.New(opt)
	if err != nil {
		fmt.Fprintf(warn, "kitties: WhatsApp stays off: %v\n", err)
		return nil
	}
	return b
}
