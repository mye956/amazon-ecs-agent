package iptableswrapper

import (
	"strconv"

	"github.com/coreos/go-iptables/iptables"
)

const (
	defaultTable = "filter"
	ruleSpec     = "-p %s --dport %s -j DROP"
)

type IPTables interface {
	NewChain(chain string) error
	Append(chain, protocol string, port uint16) error
	Insert(chain, insertChain string) error
	ChainExists(chain string) (bool, error)
	Exists(chain, protocol string, port uint16) (bool, error)
	ClearChain(chain string) error
	Delete(trafficType, chain string) error
	DeleteChain(chain string) error
	ListChains() ([]string, error)
}

type IPTablesWrapper struct {
}

func NewWrapper() *IPTablesWrapper {
	return &IPTablesWrapper{}
}

func newIPTables() (*iptables.IPTables, error) {
	return iptables.New()
}

func (ipt *IPTablesWrapper) NewChain(chain string) error {
	iptable, err := newIPTables()
	if err != nil {
		return err
	}
	return iptable.NewChain(defaultTable, chain)
}

// Wrapper function for Append()
func (ipt *IPTablesWrapper) Append(chain, protocol string, port uint16) error {
	iptable, err := newIPTables()
	if err != nil {
		return err
	}
	parameterList := []string{"-p", protocol, "--dport", strconv.FormatUint(uint64(port), 10), "-j", "DROP"}
	return iptable.Append(defaultTable, chain, parameterList...)
}

// Wrapper function for Insert()
func (ipt *IPTablesWrapper) Insert(chain, insertChain string) error {
	iptable, err := newIPTables()
	if err != nil {
		return err
	}
	parameterList := []string{"-j", chain}

	return iptable.Insert(defaultTable, insertChain, 1, parameterList...)
}

func (ipt *IPTablesWrapper) ChainExists(chain string) (bool, error) {
	return true, nil
}

func (ipt *IPTablesWrapper) Exists(chain, protocol string, port uint16) (bool, error) {
	iptable, err := newIPTables()
	if err != nil {
		return false, err
	}
	parameterList := []string{"-p", protocol, "--dport", strconv.FormatUint(uint64(port), 10), "-j", "DROP"}
	return iptable.Exists(defaultTable, chain, parameterList...)
}

func (ipt *IPTablesWrapper) ClearChain(chain string) error {
	iptable, err := newIPTables()
	if err != nil {
		return err
	}
	return iptable.ClearChain(defaultTable, chain)
}

func (ipt *IPTablesWrapper) Delete(chain, insertChain string) error {
	iptable, err := newIPTables()
	if err != nil {
		return err
	}
	parameterList := []string{"-j", chain}

	return iptable.Delete(defaultTable, insertChain, parameterList...)
}

func (ipt *IPTablesWrapper) DeleteChain(chain string) error {
	iptable, err := newIPTables()
	if err != nil {
		return err
	}

	return iptable.DeleteChain(defaultTable, chain)
}

func (ipt *IPTablesWrapper) ListChains() ([]string, error) {
	iptable, err := newIPTables()
	if err != nil {
		return []string{}, err
	}
	return iptable.ListChains("filter")
}
