// Copyright 2016 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package ens

//go:generate abigen --sol contract/ENS.sol --pkg contract --out contract/ens.go
//go:generate abigen --sol contract/ENSRegistry.sol --exc contract/ENS.sol:ENS --pkg contract --out contract/ensregistry.go
//go:generate abigen --sol contract/FIFSRegistrar.sol --exc contract/ENS.sol:ENS --pkg contract --out contract/fifsregistrar.go
//go:generate abigen --sol contract/PublicResolver.sol --exc contract/ENS.sol:ENS --pkg contract --out contract/publicresolver.go

import (
	"encoding/binary"
	"strings"

	mh "github.com/multiformats/go-multihash"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/contracts/ens/contract"
	"github.com/ethereum/go-ethereum/contracts/ens/fallback_contract"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ipfs/go-cid"
)

var (
	MainNetAddress           = common.HexToAddress("0x314159265dD8dbb310642f98f50C066173C1259b")
	TestNetAddress           = common.HexToAddress("0x112234455c3a32fd11230c42e7bccd4a84e02010")
	contentHash_Interface_Id [4]byte
)

const contentHash_Interface_Id_Spec = 0xbc1c58d1

func init() {
	binary.BigEndian.PutUint32(contentHash_Interface_Id[:], contentHash_Interface_Id_Spec)
}

// ENS is the swarm domain name registry and resolver
type ENS struct {
	*contract.ENSSession
	contractBackend bind.ContractBackend
}

// NewENS creates a struct exposing convenient high-level operations for interacting with
// the Ethereum Name Service.
func NewENS(transactOpts *bind.TransactOpts, contractAddr common.Address, contractBackend bind.ContractBackend) (*ENS, error) {
	ens, err := contract.NewENS(contractAddr, contractBackend)
	if err != nil {
		return nil, err
	}
	return &ENS{
		&contract.ENSSession{
			Contract:     ens,
			TransactOpts: *transactOpts,
		},
		contractBackend,
	}, nil
}

// DeployENS deploys an instance of the ENS nameservice, with a 'first-in, first-served' root registrar.
func DeployENS(transactOpts *bind.TransactOpts, contractBackend bind.ContractBackend) (common.Address, *ENS, error) {
	// Deploy the ENS registry
	ensAddr, _, _, err := contract.DeployENSRegistry(transactOpts, contractBackend)
	if err != nil {
		return ensAddr, nil, err
	}
	ens, err := NewENS(transactOpts, ensAddr, contractBackend)
	if err != nil {
		return ensAddr, nil, err
	}
	// Deploy the registrar
	regAddr, _, _, err := contract.DeployFIFSRegistrar(transactOpts, contractBackend, ensAddr, [32]byte{})
	if err != nil {
		return ensAddr, nil, err
	}
	// Set the registrar as owner of the ENS root
	if _, err = ens.SetOwner([32]byte{}, regAddr); err != nil {
		return ensAddr, nil, err
	}
	return ensAddr, ens, nil
}

func ensParentNode(name string) (common.Hash, common.Hash) {
	parts := strings.SplitN(name, ".", 2)
	label := crypto.Keccak256Hash([]byte(parts[0]))
	if len(parts) == 1 {
		return [32]byte{}, label
	}
	parentNode, parentLabel := ensParentNode(parts[1])
	return crypto.Keccak256Hash(parentNode[:], parentLabel[:]), label
}

func EnsNode(name string) common.Hash {
	parentNode, parentLabel := ensParentNode(name)
	return crypto.Keccak256Hash(parentNode[:], parentLabel[:])
}

func (ens *ENS) getResolver(node [32]byte) (*contract.PublicResolverSession, error) {
	resolverAddr, err := ens.Resolver(node)
	if err != nil {
		return nil, err
	}
	resolver, err := contract.NewPublicResolver(resolverAddr, ens.contractBackend)
	if err != nil {
		return nil, err
	}
	return &contract.PublicResolverSession{
		Contract:     resolver,
		TransactOpts: ens.TransactOpts,
	}, nil
}

func (ens *ENS) getFallbackResolver(node [32]byte) (*fallback_contract.PublicResolverSession, error) {
	resolverAddr, err := ens.Resolver(node)
	if err != nil {
		return nil, err
	}
	resolver, err := fallback_contract.NewPublicResolver(resolverAddr, ens.contractBackend)
	if err != nil {
		return nil, err
	}
	return &fallback_contract.PublicResolverSession{
		Contract:     resolver,
		TransactOpts: ens.TransactOpts,
	}, nil
}

func (ens *ENS) getRegistrar(node [32]byte) (*contract.FIFSRegistrarSession, error) {
	registrarAddr, err := ens.Owner(node)
	if err != nil {
		return nil, err
	}
	registrar, err := contract.NewFIFSRegistrar(registrarAddr, ens.contractBackend)
	if err != nil {
		return nil, err
	}
	return &contract.FIFSRegistrarSession{
		Contract:     registrar,
		TransactOpts: ens.TransactOpts,
	}, nil
}

// Resolve is a non-transactional call that returns the content hash associated with a name.
func (ens *ENS) Resolve(name string) (common.Hash, error) {
	node := EnsNode(name)

	resolver, err := ens.getResolver(node)
	if err != nil {
		return common.Hash{}, err
	}

	// IMPORTANT: The old contract is deprecated. This code should be removed latest on June 1st 2019
	supported, err := resolver.SupportsInterface(contentHash_Interface_Id)
	if err != nil {
		return common.Hash{}, err
	}

	if !supported {
		resolver, err := ens.getFallbackResolver(node)
		if err != nil {
			return common.Hash{}, err
		}
		ret, err := resolver.Content(node)
		if err != nil {
			return common.Hash{}, err
		}
		return common.BytesToHash(ret[:]), nil
	}

	// END DEPRECATED CODE

	ret, err := resolver.Contenthash(node)
	if err != nil {
		return common.Hash{}, err
	}
	return common.BytesToHash(ret[:]), nil
}

// Addr is a non-transactional call that returns the address associated with a name.
func (ens *ENS) Addr(name string) (common.Address, error) {
	node := EnsNode(name)

	resolver, err := ens.getResolver(node)
	if err != nil {
		return common.Address{}, err
	}
	ret, err := resolver.Addr(node)
	if err != nil {
		return common.Address{}, err
	}
	return common.BytesToAddress(ret[:]), nil
}

// SetAddress sets the address associated with a name. Only works if the caller
// owns the name, and the associated resolver implements a `setAddress` function.
func (ens *ENS) SetAddr(name string, addr common.Address) (*types.Transaction, error) {
	node := EnsNode(name)

	resolver, err := ens.getResolver(node)
	if err != nil {
		return nil, err
	}
	opts := ens.TransactOpts
	opts.GasLimit = 200000
	return resolver.Contract.SetAddr(&opts, node, addr)
}

// Register registers a new domain name for the caller, making them the owner of the new name.
// Only works if the registrar for the parent domain implements the FIFS registrar protocol.
func (ens *ENS) Register(name string) (*types.Transaction, error) {
	parentNode, label := ensParentNode(name)
	registrar, err := ens.getRegistrar(parentNode)
	if err != nil {
		return nil, err
	}
	return registrar.Contract.Register(&ens.TransactOpts, label, ens.TransactOpts.From)
}

// SetContentHash sets the content hash associated with a name. Only works if the caller
// owns the name, and the associated resolver implements a `setContenthash` function.
func (ens *ENS) SetContentHash(name string, hash []byte) (*types.Transaction, error) {
	node := EnsNode(name)

	resolver, err := ens.getResolver(node)
	if err != nil {
		return nil, err
	}

	opts := ens.TransactOpts
	opts.GasLimit = 200000

	// IMPORTANT: The old contract is deprecated. This code should be removed latest on June 1st 2019
	supported, err := resolver.SupportsInterface(contentHash_Interface_Id)
	if err != nil {
		return nil, err
	}

	if !supported {
		resolver, err := ens.getFallbackResolver(node)
		if err != nil {
			return nil, err
		}
		opts := ens.TransactOpts
		opts.GasLimit = 200000
		var b [32]byte
		copy(b[:], hash)
		return resolver.Contract.SetContent(&opts, node, b)
	}

	// END DEPRECATED CODE
	return resolver.Contract.SetContenthash(&opts, node, hash)
}

func decodeMultiCodec(b []byte) (common.Hash, error) {

	// Create a cid from a marshaled string
	c, err := cid.Decode(string(b))
	if err != nil {
		return common.Hash{}, err
	}

	return common.Hash{}, nil
	/* from the EIP documentation
	   storage system: Swarm (0xe4)
	   CID version: 1 (0x01)
	   content type: swarm-manifest (0x??)
	   hash function: keccak-256 (0x1B)
	   hash length: 32 bytes (0x20)
	   hash: 29f2d17be6139079dc48696d1f582a8530eb9805b561eda517e22a892c7e3f1f
	*/
	//<protoCode uvarint><cid-version><multicodec-content-type><multihash-content-address>

}

// encodeCid encodes a swarm hash into an IPLD CID
func encodeCid(h common.Hash) (cid.Cid, error) {
	b := []byte{0x1b, 0x20}     //0x1b = keccak256 (should be changed to bmt), 0x20 = 32 bytes hash length
	b = append(b, h.Bytes()...) // append actual hash bytes
	multi, err := mh.Cast(b)
	if err != nil {
		return cid.Cid{}, err
	}

	c := cid.NewCidV1(cid.Raw, multi) //todo: cid.Raw should be swarm manifest

	return c, nil
}
