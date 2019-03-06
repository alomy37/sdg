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

import (
	"bytes"
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/accounts/abi/bind/backends"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/contracts/ens/contract"
	"github.com/ethereum/go-ethereum/contracts/ens/fallback_contract"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/crypto"
	mh "github.com/multiformats/go-multihash"
)

var (
	key, _       = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	name         = "my name on ENS"
	hash         = crypto.Keccak256Hash([]byte("my content"))
	fallbackHash = crypto.Keccak256Hash([]byte("my content hash"))
	addr         = crypto.PubkeyToAddress(key.PublicKey)
	testAddr     = common.HexToAddress("0x1234123412341234123412341234123412341234")
)

func TestENS(t *testing.T) {
	contractBackend := backends.NewSimulatedBackend(core.GenesisAlloc{addr: {Balance: big.NewInt(1000000000)}}, 10000000)
	transactOpts := bind.NewKeyedTransactor(key)

	ensAddr, ens, err := DeployENS(transactOpts, contractBackend)
	if err != nil {
		t.Fatalf("can't deploy root registry: %v", err)
	}
	contractBackend.Commit()

	// Set ourself as the owner of the name.
	if _, err := ens.Register(name); err != nil {
		t.Fatalf("can't register: %v", err)
	}
	contractBackend.Commit()

	// Deploy a resolver and make it responsible for the name.
	resolverAddr, _, _, err := contract.DeployPublicResolver(transactOpts, contractBackend, ensAddr)
	if err != nil {
		t.Fatalf("can't deploy resolver: %v", err)
	}
	if _, err := ens.SetResolver(EnsNode(name), resolverAddr); err != nil {
		t.Fatalf("can't set resolver: %v", err)
	}
	contractBackend.Commit()

	// Set the content hash for the name.
	if _, err = ens.SetContentHash(name, hash.Bytes()); err != nil {
		t.Fatalf("can't set content hash: %v", err)
	}
	contractBackend.Commit()

	// Try to resolve the name.
	vhost, err := ens.Resolve(name)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if vhost != hash {
		t.Fatalf("resolve error, expected %v, got %v", hash.Hex(), vhost.Hex())
	}

	// set the address for the name
	if _, err = ens.SetAddr(name, testAddr); err != nil {
		t.Fatalf("can't set address: %v", err)
	}
	contractBackend.Commit()

	// Try to resolve the name to an address
	recoveredAddr, err := ens.Addr(name)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if testAddr != recoveredAddr {
		t.Fatalf("resolve error, expected %v, got %v", testAddr.Hex(), recoveredAddr.Hex())
	}

	// deploy the fallback contract and see that the fallback mechanism works
	fallbackResolverAddr, _, _, err := fallback_contract.DeployPublicResolver(transactOpts, contractBackend, ensAddr)
	if err != nil {
		t.Fatalf("can't deploy resolver: %v", err)
	}
	if _, err := ens.SetResolver(EnsNode(name), fallbackResolverAddr); err != nil {
		t.Fatalf("can't set resolver: %v", err)
	}
	contractBackend.Commit()

	// Set the content hash for the name.
	if _, err = ens.SetContentHash(name, fallbackHash.Bytes()); err != nil {
		t.Fatalf("can't set content hash: %v", err)
	}
	contractBackend.Commit()

	// Try to resolve the name.
	vhost, err = ens.Resolve(name)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if vhost != fallbackHash {
		t.Fatalf("resolve error, expected %v, got %v", hash.Hex(), vhost.Hex())
	}
	t.Fatal("todo: try to set old contract with new multicodec stuff and assert fail, set new contract with multicodec stuff, encode, decode and assert returns correct hash")
}

func TestCIDSanity(t *testing.T) {
	for _, v := range []struct {
		name    string
		hashStr string
		fail    bool
	}{
		{
			name:    "hash OK, should not fail",
			hashStr: "d1de9994b4d039f6548d191eb26786769f580809256b4685ef316805265ea162",
			fail:    false,
		},
		{
			name:    "hash empty , should fail",
			hashStr: "",
			fail:    true,
		},
	} {
		t.Run(v.name, func(t *testing.T) {
			hash := common.HexToHash(v.hashStr)
			cc, err := encodeCid(hash)
			if err != nil {
				if v.fail {
					return
				}
				t.Fatal(err)
			}

			if cc.Prefix().MhLength != 32 {
				t.Fatal("w00t")
			}
			fmt.Println(cc.Hash())
			decoded, err := mh.Decode(cc.Hash())
			if err != nil {
				t.Fatal(err)
			}
			if decoded.Length != 32 {
				t.Fatal("invalid length")
			}
			if !bytes.Equal(decoded.Digest, hash[:]) {
				t.Fatalf("hashes not equal")
			}

			if decoded.Length != 32 {
				t.Fatal("wrong length")
			}
			fmt.Println("Created CID: ", cc)

		})

		/*c, err := cid.Decode("zdvgqEMYmNeH5fKciougvQcfzMcNjF3Z1tPouJ8C7pc3pe63k")
		if err != nil {
			t.Fatal("Error decoding CID")
		}

		fmt.Sprintf("Got CID: %v", c)
		fmt.Println("Got CID:", c.Prefix())
		*/
	}
}
