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

package kademlia

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
)

//metrics variables
//For metrics, we want to count how many times peers are added/removed
//at a certain index. Thus we do that with an array of counters with
//entry for each index
var (
	bucketAddIndexCount []metrics.Counter
	bucketRmIndexCount  []metrics.Counter
)

const (
	bucketSize   = 4
	proxBinSize  = 2
	maxProx      = 8
	connRetryExp = 2
	maxPeers     = 100
)

var (
	purgeInterval        = 42 * time.Hour
	initialRetryInterval = 42 * time.Millisecond
	maxIdleInterval      = 42 * 1000 * time.Millisecond
	// maxIdleInterval      = 42 * 10	0 * time.Millisecond
)

type KadParams struct {
	// adjustable parameters
	MaxProx              int
	ProxBinSize          int
	BucketSize           int
	PurgeInterval        time.Duration
	InitialRetryInterval time.Duration
	MaxIdleInterval      time.Duration
	ConnRetryExp         int
}

func NewDefaultKadParams() *KadParams {
	return &KadParams{
		MaxProx:              maxProx,
		ProxBinSize:          proxBinSize,
		BucketSize:           bucketSize,
		PurgeInterval:        purgeInterval,
		InitialRetryInterval: initialRetryInterval,
		MaxIdleInterval:      maxIdleInterval,
		ConnRetryExp:         connRetryExp,
	}
}

// Kademlia is a table of active nodes
type Kademlia struct {
	addr       Address      // immutable baseaddress of the table
	*KadParams              // Kademlia configuration parameters
	proxLimit  int          // state, the PO of the first row of the most proximate bin
	proxSize   int          // state, the number of peers in the most proximate bin
	count      int          // number of active peers (w live connection)
	buckets    [][]Node     // the actual bins
	db         *KadDb       // kaddb, node record database
	lock       sync.RWMutex // mutex to access buckets
}

type Node interface {
	Addr() Address
	URL() string
	LastActive() time.Time
	Drop()
}

// New is a public constructor. Addr  is the base address of the table 
// and params is KadParams configuration.
func New(addr Address, params *KadParams) *Kademlia {
	buckets := make([][]Node, params.MaxProx+1)
	kad := &Kademlia{
		addr:      addr,
		KadParams: params,
		buckets:   buckets,
		db:        newKadDb(addr, params),
	}
	kad.initMetricsVariables()
	return kad
}

// Addr is the accessor for KAD base address.
func (k *Kademlia) Addr() Address {
	return k.addr
}

// Count is the accessor for KAD active node count.
func (k *Kademlia) Count() int {
	defer k.lock.Unlock()
	k.lock.Lock()
	return k.count
}

// DBCount is nthe accessor for KAD active node count.
func (k *Kademlia) DBCount() int {
	return k.db.count()
}

// On is the entry point called when a new nodes is added
// unsafe in that node is not checked to be already active node (to be called once)
func (k *Kademlia) On(node Node, cb func(*NodeRecord, Node) error) (err error) {
	log.Debug(fmt.Sprintf("%v", k))
	defer k.lock.Unlock()
	k.lock.Lock()

	index := k.proximityBin(node.Addr())
	record := k.db.findOrCreate(index, node.Addr(), node.URL())

	if cb != nil {
		err = cb(record, node)
		log.Trace(fmt.Sprintf("cb(%v, %v) ->%v", record, node, err))
		if err != nil {
			return fmt.Errorf("unable to add node %v, callback error: %v", node.Addr(), err)
		}
		log.Debug(fmt.Sprintf("add node record %v with node %v", record, node))
	}

	// insert in kademlia table of active nodes
	bucket := k.buckets[index]
	// if bucket is full insertion replaces the worst node
	// TODO: give priority to peers with active traffic
	if len(bucket) < k.BucketSize { // >= allows us to add peers beyond the bucketsize limitation
		k.buckets[index] = append(bucket, node)
		bucketAddIndexCount[index].Inc(1)
		log.Debug(fmt.Sprintf("add node %v to table", node))
		k.setProxLimit(index, true)
		record.node = node
		k.count++
		return nil
	}

	// always rotate peers
	idle := k.MaxIdleInterval
	var pos int
	var replaced Node
	for i, p := range bucket {
		idleInt := time.Since(p.LastActive())
		if idleInt > idle {
			idle = idleInt
			pos = i
			replaced = p
		}
	}
	if replaced == nil {
		log.Debug(fmt.Sprintf("all peers wanted, PO%03d bucket full", index))
		return fmt.Errorf("bucket full")
	}
	log.Debug(fmt.Sprintf("node %v replaced by %v (idle for %v  > %v)", replaced, node, idle, k.MaxIdleInterval))
	replaced.Drop()
	// actually replace in the row. When off(node) is called, the peer is no longer in the row
	bucket[pos] = node
	// there is no change in bucket cardinalities so no prox limit adjustment is needed
	record.node = node
	k.count++
	return nil
}

// Off is the called when a node is taken offline (from the protocol main loop exit)
func (k *Kademlia) Off(node Node, cb func(*NodeRecord, Node)) (err error) {
	k.lock.Lock()
	defer k.lock.Unlock()

	index := k.proximityBin(node.Addr())
	bucketRmIndexCount[index].Inc(1)
	bucket := k.buckets[index]
	for i := 0; i < len(bucket); i++ {
		if node.Addr() == bucket[i].Addr() {
			k.buckets[index] = append(bucket[:i], bucket[(i+1):]...)
			k.setProxLimit(index, false)
			break
		}
	}

	record := k.db.index[node.Addr()]
	// callback on remove
	if cb != nil {
		cb(record, record.node)
	}
	record.node = nil
	k.count--
	log.Debug(fmt.Sprintf("remove node %v from table, population now is %v", node, k.count))

	return
}

// proxLimit is dynamically adjusted so that
// 1) there is no empty buckets in bin < proxLimit and
// 2) the sum of all items are the minimum possible but higher than ProxBinSize
// adjust Prox (proxLimit and proxSize after an insertion/removal of nodes)
// caller holds the lock
func (k *Kademlia) setProxLimit(r int, on bool) {
	// if the change is outside the core (PO lower)
	// and the change does not leave a bucket empty then
	// no adjustment needed
	if r < k.proxLimit && len(k.buckets[r]) > 0 {
		return
	}
	// if on=a node was added, then r must be within prox limit so increment cardinality
	if on {
		k.proxSize++
		curr := len(k.buckets[k.proxLimit])
		// if now core is big enough without the furthest bucket, then contract
		// this can result in more than one bucket change
		for k.proxSize >= k.ProxBinSize+curr && curr > 0 {
			k.proxSize -= curr
			k.proxLimit++
			curr = len(k.buckets[k.proxLimit])

			log.Trace(fmt.Sprintf("proxbin contraction (size: %v, limit: %v, bin: %v)", k.proxSize, k.proxLimit, r))
		}
		return
	}
	// otherwise
	if r >= k.proxLimit {
		k.proxSize--
	}
	// expand core by lowering prox limit until hit zero or cover the empty bucket or reached target cardinality
	for (k.proxSize < k.ProxBinSize || r < k.proxLimit) &&
		k.proxLimit > 0 {
		//
		k.proxLimit--
		k.proxSize += len(k.buckets[k.proxLimit])
		log.Trace(fmt.Sprintf("proxbin expansion (size: %v, limit: %v, bin: %v)", k.proxSize, k.proxLimit, r))
	}
}

/*
FindClosest returns the list of nodes belonging to the same proximity bin
as the target. The most proximate bin will be the union of the bins between
proxLimit and MaxProx.
*/
func (k *Kademlia) FindClosest(target Address, max int) []Node {
	k.lock.Lock()
	defer k.lock.Unlock()

	r := nodesByDistance{
		target: target,
	}

	po := k.proximityBin(target)
	index := po
	step := 1
	log.Trace(fmt.Sprintf("serving %v nodes at %v (PO%02d)", max, index, po))

	// if max is set to 0, just want a full bucket, dynamic number
	min := max
	// set limit to max
	limit := max
	if max == 0 {
		min = 1
		limit = maxPeers
	}

	var n int
	for index >= 0 {
		// add entire bucket
		for _, p := range k.buckets[index] {
			r.push(p, limit)
			n++
		}
		// terminate if index reached the bottom or enough peers > min
		log.Trace(fmt.Sprintf("add %v -> %v (PO%02d, PO%03d)", len(k.buckets[index]), n, index, po))
		if n >= min && (step < 0 || max == 0) {
			break
		}
		// reach top most non-empty PO bucket, turn around
		if index == k.MaxProx {
			index = po
			step = -1
		}
		index += step
	}
	log.Trace(fmt.Sprintf("serve %d (<=%d) nodes for target lookup %v (PO%03d)", n, max, target, po))
	return r.nodes
}

func (k *Kademlia) Suggest() (*NodeRecord, bool, int) {
	defer k.lock.RUnlock()
	k.lock.RLock()
	return k.db.findBest(k.BucketSize, func(i int) int { return len(k.buckets[i]) })
}

// Add adds node records to kaddb (persisted node record db)
func (k *Kademlia) Add(nrs []*NodeRecord) {
	k.db.add(nrs, k.proximityBin)
}

// nodesByDistance is a list of nodes, ordered by distance to target.
type nodesByDistance struct {
	nodes  []Node
	target Address
}

func sortedByDistanceTo(target Address, slice []Node) bool {
	var last Address
	for i, node := range slice {
		if i > 0 {
			if target.ProxCmp(node.Addr(), last) < 0 {
				return false
			}
		}
		last = node.Addr()
	}
	return true
}

// push(node, max) adds the given node to the list, keeping the total size
// below max elements.
func (h *nodesByDistance) push(node Node, max int) {
	// returns the firt index ix such that func(i) returns true
	ix := sort.Search(len(h.nodes), func(i int) bool {
		return h.target.ProxCmp(h.nodes[i].Addr(), node.Addr()) >= 0
	})

	if len(h.nodes) < max {
		h.nodes = append(h.nodes, node)
	}
	if ix < len(h.nodes) {
		copy(h.nodes[ix+1:], h.nodes[ix:])
		h.nodes[ix] = node
	}
}

/*
Taking the proximity order relative to a fix point x classifies the points in
the space (n byte long byte sequences) into bins. Items in each are at
most half as distant from x as items in the previous bin. Given a sample of
uniformly distributed items (a hash function over arbitrary sequence) the
proximity scale maps onto series of subsets with cardinalities on a negative
exponential scale.

It also has the property that any two item belonging to the same bin are at
most half as distant from each other as they are from x.

If we think of random sample of items in the bins as connections in a network of interconnected nodes than relative proximity can serve as the basis for local
decisions for graph traversal where the task is to find a route between two
points. Since in every hop, the finite distance halves, there is
a guaranteed constant maximum limit on the number of hops needed to reach one
node from the other.
*/

func (k *Kademlia) proximityBin(other Address) (ret int) {
	ret = proximity(k.addr, other)
	if ret > k.MaxProx {
		ret = k.MaxProx
	}
	return
}

// KeyRange provides the keyrange for chunk db iteration.
func (k *Kademlia) KeyRange(other Address) (start, stop Address) {
	defer k.lock.RUnlock()
	k.lock.RLock()
	return KeyRange(k.addr, other, k.proxLimit)
}

// Save persists kaddb on disk (written to file on path in json format.
func (k *Kademlia) Save(path string, cb func(*NodeRecord, Node)) error {
	return k.db.save(path, cb)
}

// Load loads the node record database (kaddb) from file on path.
func (k *Kademlia) Load(path string, cb func(*NodeRecord, Node) error) (err error) {
	return k.db.load(path, cb)
}

// kademlia table + kaddb table displayed with ascii
func (k *Kademlia) String() string {
	defer k.lock.RUnlock()
	k.lock.RLock()
	defer k.db.lock.RUnlock()
	k.db.lock.RLock()

	var rows []string
	rows = append(rows, "=========================================================================")
	rows = append(rows, fmt.Sprintf("%v KΛÐΞMLIΛ hive: queen's address: %v", time.Now().UTC().Format(time.UnixDate), k.addr.String()[:6]))
	rows = append(rows, fmt.Sprintf("population: %d (%d), proxLimit: %d, proxSize: %d", k.count, len(k.db.index), k.proxLimit, k.proxSize))
	rows = append(rows, fmt.Sprintf("MaxProx: %d, ProxBinSize: %d, BucketSize: %d", k.MaxProx, k.ProxBinSize, k.BucketSize))

	for i, bucket := range k.buckets {

		if i == k.proxLimit {
			rows = append(rows, fmt.Sprintf("============ PROX LIMIT: %d ==========================================", i))
		}
		row := []string{fmt.Sprintf("%03d", i), fmt.Sprintf("%2d", len(bucket))}
		var inc int
		c := k.db.cursors[i]
		for ; inc < len(bucket); inc++ {
			p := bucket[(c+inc)%len(bucket)]
			row = append(row, p.Addr().String()[:6])
			if inc == 4 {
				break
			}
		}
		for ; inc < 4; inc++ {
			row = append(row, "      ")
		}
		row = append(row, fmt.Sprintf("| %2d %2d", len(k.db.Nodes[i]), k.db.cursors[i]))

		for j, p := range k.db.Nodes[i] {
			row = append(row, p.Addr.String()[:6])
			if j == 3 {
				break
			}
		}
		rows = append(rows, strings.Join(row, " "))
		if i == k.MaxProx {
		}
	}
	rows = append(rows, "=========================================================================")
	return strings.Join(rows, "\n")
}

//We have to build up the array of counters for each index
func (k *Kademlia) initMetricsVariables() {
	//create the arrays
	bucketAddIndexCount = make([]metrics.Counter, k.MaxProx+1)
	bucketRmIndexCount = make([]metrics.Counter, k.MaxProx+1)
	//at each index create a metrics counter
	for i := 0; i < (k.KadParams.MaxProx + 1); i++ {
		bucketAddIndexCount[i] = metrics.NewRegisteredCounter(fmt.Sprintf("network.kademlia.bucket.add.%d.index", i), nil)
		bucketRmIndexCount[i] = metrics.NewRegisteredCounter(fmt.Sprintf("network.kademlia.bucket.rm.%d.index", i), nil)
	}
}
