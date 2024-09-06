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
	Insert(trafficType, chain, insertChain string) error
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
	// ruleSpec := []string{"-p", protocol, "--dport", strconv.FormatUint(uint64(port), 10)}
	return nil
}

// Wrapper function for Insert()
func (ipt *IPTablesWrapper) Insert(trafficType, chain, insertChain string) error {
	return nil
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
	return nil
}

func (ipt *IPTablesWrapper) Delete(trafficType, chain string) error {
	return nil
}

func (ipt *IPTablesWrapper) DeleteChain(chain string) error {
	return nil
}

func (ipt *IPTablesWrapper) ListChains() ([]string, error) {
	iptable, err := newIPTables()
	if err != nil {
		return []string{}, err
	}
	return iptable.ListChains("filter")
}
