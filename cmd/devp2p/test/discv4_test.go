package test

import (
	"crypto/ecdsa"
	"flag"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/p2p/discover/v4wire"
	"github.com/ethereum/go-ethereum/p2p/enode"
)

const (
	expiration  = 20 * time.Second
	wrongPacket = 66
	macSize     = 256 / 8
)

var (
	enodeID  = flag.String("enode", "", "enode:... as per `admin.nodeInfo.enode`")
	remote   = flag.String("remote", "127.0.0.1:30303", "")
	waitTime = flag.Int("waitTime", 500, "ms to wait for response")

	remoteAddr        *net.UDPAddr
	localhost         = net.ParseIP("127.0.0.1")
	localhostEndpoint = v4wire.Endpoint{IP: localhost}
	remoteEndpoint    v4wire.Endpoint
	wrongEndpoint     = v4wire.Endpoint{IP: net.ParseIP("192.0.2.0")}
	priv              *ecdsa.PrivateKey
)

type pingWithJunk struct {
	Version    uint
	From, To   v4wire.Endpoint
	Expiration uint64
	JunkData1  uint
	JunkData2  []byte
}

func (req *pingWithJunk) Name() string { return "PING/v4" }
func (req *pingWithJunk) Kind() byte   { return v4wire.PingPacket }

type pingWrongType struct {
	Version    uint
	From, To   v4wire.Endpoint
	Expiration uint64
}

func (req *pingWrongType) Name() string { return "WRONG/v4" }
func (req *pingWrongType) Kind() byte   { return wrongPacket }

func TestMain(m *testing.M) {
	if os.Getenv("CI") != "" {
		os.Exit(0)
	}

	flag.Parse()

	var err error
	remoteAddr, err = net.ResolveUDPAddr("udp", *remote)
	if err != nil {
		panic(err)
	}
	remoteEndpoint = v4wire.Endpoint{IP: remoteAddr.IP}

	priv, err = crypto.GenerateKey()
	if err != nil {
		panic(err)
	}

	os.Exit(m.Run())
}

func futureExpiration() uint64 {
	return uint64(time.Now().Add(expiration).Unix())
}

func sendPacket(c *net.UDPConn, req v4wire.Packet) error {
	packet, _, err := v4wire.Encode(priv, req)
	if err != nil {
		return err
	}

	n, err := c.Write(packet)
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("0 byte written")
	}
	return nil
}

func readPacket(c *net.UDPConn) (v4wire.Packet, error) {
	buf := make([]byte, 2048)
	var err error
	if err = c.SetReadDeadline(time.Now().Add(time.Duration(*waitTime) * time.Millisecond)); err != nil {
		return nil, err
	}
	n, err := c.Read(buf)
	if err != nil {
		return nil, err
	}
	p, _, _, err := v4wire.Decode(buf[:n])
	if err != nil {
		return nil, err
	}
	return p, nil
}

func readFromPacketConn(c net.PacketConn) (v4wire.Packet, error) {
	buf := make([]byte, 2048)
	var err error

	if err = c.SetReadDeadline(time.Now().Add(time.Duration(*waitTime) * time.Millisecond)); err != nil {
		return nil, err
	}
	n, _, err := c.ReadFrom(buf)
	if err != nil {
		return nil, err
	}
	p, _, _, err := v4wire.Decode(buf[:n])
	if err != nil {
		return nil, err
	}
	return p, nil
}

func writeToPacketConn(c net.PacketConn, req v4wire.Packet, addr net.Addr) error {
	packet, _, err := v4wire.Encode(priv, req)
	if err != nil {
		return err
	}

	n, err := c.WriteTo(packet, addr)
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("0 byte written")
	}
	return nil
}

func PingKnownEnode(t *testing.T) {
	c, err := net.DialUDP("udp", nil, remoteAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	req := v4wire.Ping{
		Version:    4,
		From:       localhostEndpoint,
		To:         remoteEndpoint,
		Expiration: futureExpiration(),
	}
	if err := sendPacket(c, &req); err != nil {
		t.Fatal("send", err)
	}
	reply, err := readPacket(c)
	if err != nil {
		t.Fatal("read", err)
	}
	if reply.Kind() != v4wire.PongPacket {
		t.Error("Reply is not a Pong", reply.Name())
	}
}

func PingWrongTo(t *testing.T) {
	c, err := net.DialUDP("udp", nil, remoteAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	req := v4wire.Ping{
		Version:    4,
		From:       localhostEndpoint,
		To:         wrongEndpoint,
		Expiration: futureExpiration(),
	}
	if err := sendPacket(c, &req); err != nil {
		t.Fatal("send", err)
	}
	reply, err := readPacket(c)
	if err != nil {
		t.Fatal("read", err)
	}
	if reply.Kind() != v4wire.PongPacket {
		t.Error("Reply is not a Pong", reply.Name())
	}
}

func PingWrongFrom(t *testing.T) {
	c, err := net.DialUDP("udp", nil, remoteAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	req := v4wire.Ping{
		Version:    4,
		From:       wrongEndpoint,
		To:         remoteEndpoint,
		Expiration: futureExpiration(),
	}
	if err := sendPacket(c, &req); err != nil {
		t.Fatal("send", err)
	}
	reply, err := readPacket(c)
	if err != nil {
		t.Fatal("read", err)
	}
	if reply.Kind() != v4wire.PongPacket {
		t.Error("Reply is not a Pong", reply.Name())
	}
}

func PingExtraData(t *testing.T) {
	c, err := net.DialUDP("udp", nil, remoteAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	req := pingWithJunk{
		Version:    4,
		From:       localhostEndpoint,
		To:         remoteEndpoint,
		Expiration: futureExpiration(),
		JunkData1:  42,
		JunkData2:  []byte{9, 8, 7, 6, 5, 4, 3, 2, 1},
	}
	if err := sendPacket(c, &req); err != nil {
		t.Fatal("send", err)
	}
	reply, err := readPacket(c)
	if err != nil {
		t.Fatal("read", err)
	}
	if reply.Kind() != v4wire.PongPacket {
		t.Error("Reply is not a Pong", reply.Name())
	}
}

func PingExtraDataWrongFrom(t *testing.T) {
	c, err := net.DialUDP("udp", nil, remoteAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	req := pingWithJunk{
		Version:    4,
		From:       wrongEndpoint,
		To:         remoteEndpoint,
		Expiration: futureExpiration(),
		JunkData1:  42,
		JunkData2:  []byte{9, 8, 7, 6, 5, 4, 3, 2, 1},
	}
	if err := sendPacket(c, &req); err != nil {
		t.Fatal("send", err)
	}
	reply, err := readPacket(c)
	if err != nil {
		t.Fatal("read", err)
	}
	if reply.Kind() != v4wire.PongPacket {
		t.Error("Reply is not a Pong", reply.Name())
	}
}

func PingPastExpiration(t *testing.T) {
	c, err := net.DialUDP("udp", nil, remoteAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	req := v4wire.Ping{
		Version:    4,
		From:       localhostEndpoint,
		To:         remoteEndpoint,
		Expiration: -futureExpiration(),
	}
	if err := sendPacket(c, &req); err != nil {
		t.Fatal("send", err)
	}
	reply, _ := readPacket(c)
	if reply != nil {
		t.Fatal("Expected no reply, got", reply)
	}
}

func WrongPacketType(t *testing.T) {
	c, err := net.DialUDP("udp", nil, remoteAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	req := pingWrongType{
		Version:    4,
		From:       localhostEndpoint,
		To:         remoteEndpoint,
		Expiration: futureExpiration(),
	}
	if err := sendPacket(c, &req); err != nil {
		t.Fatal("send", err)
	}
	reply, _ := readPacket(c)
	if reply != nil {
		t.Fatal("Expected no reply, got", reply)
	}
}

func SourceKnownPingFromSignatureMismatch(t *testing.T) {
	var reply v4wire.Packet
	var err error
	var c *net.UDPConn

	c, err = net.DialUDP("udp", nil, remoteAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	req := v4wire.Ping{
		Version:    4,
		From:       localhostEndpoint,
		To:         remoteEndpoint,
		Expiration: futureExpiration(),
	}
	if err = sendPacket(c, &req); err != nil {
		t.Fatal("send", err)
	}

	//hang around for a bit (we don't know if the target was already bonded or not)
	time.Sleep(2 * time.Second)

	req2 := v4wire.Ping{
		Version:    4,
		From:       wrongEndpoint,
		To:         remoteEndpoint,
		Expiration: futureExpiration(),
	}
	if err = sendPacket(c, &req2); err != nil {
		t.Fatal("send 2nd", err)
	}
	reply, err = readPacket(c)
	if err != nil {
		t.Fatal("read 2nd", err)
	}
	if reply.Kind() != v4wire.PongPacket {
		t.Error("Reply is not a Pong after bonding", reply.Name())
	}
}

func FindNeighbours(t *testing.T) {
	var err error
	var c *net.UDPConn
	var reply v4wire.Packet

	c, err = net.DialUDP("udp", nil, remoteAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	targetNode := enode.MustParseV4(*enodeID)
	targetEncKey := v4wire.EncodePubkey(targetNode.Pubkey())
	findReq := v4wire.Findnode{
		Target:     targetEncKey,
		Expiration: futureExpiration(),
	}
	if err = sendPacket(c, &findReq); err != nil {
		t.Fatal("sending find nodes", err)
	}

	reply, _ = readPacket(c)
	if reply != nil && reply.Kind() != v4wire.PingPacket {
		t.Fatal("Expected timeout or ping, got", reply)
	}
}

func SpoofSanityCheck(t *testing.T) {
	var err error
	var reply v4wire.Packet

	conn, err := net.ListenPacket("udp", "127.0.0.2:0")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	req := v4wire.Ping{
		Version:    4,
		From:       localhostEndpoint,
		To:         remoteEndpoint,
		Expiration: futureExpiration(),
	}
	if err := writeToPacketConn(conn, &req, remoteAddr); err != nil {
		t.Fatal(err)
	}

	// We expect the relayConn to receive a pong
	reply, err = readFromPacketConn(conn)
	if err != nil {
		t.Fatal("read", err)
	}
	if reply.Kind() != v4wire.PongPacket {
		t.Error("Reply is not a Pong", reply.Name())
	}
}

// spoofed ping victim -> target
// (pong target -> victim)
// (ping target -> victim)
// wait
// spoofed pong victim -> target
// spoofed findnode victim -> target
// (target should ignore it)
func SpoofAmplificationAttackCheck(t *testing.T) {
	var err error

	victimConn, err := net.ListenPacket("udp", "127.0.0.2:0")
	if err != nil {
		t.Fatal(err)
	}
	defer victimConn.Close()

	// send ping
	pingReq := v4wire.Ping{
		Version:    4,
		From:       localhostEndpoint,
		To:         remoteEndpoint,
		Expiration: futureExpiration(),
	}
	if err := writeToPacketConn(victimConn, &pingReq, remoteAddr); err != nil {
		t.Fatal(err)
	}

	//wait for a tiny bit
	//NB: in a real scenario the 'victim' will have responded with a v4 pong
	//message to our ping recipient. in the attack scenario, the pong
	//will have been ignored because the source id is different than
	//expected. (to be more authentic, an improvement to this test
	//could be to send a fake pong from the node id - but this is not
	//essential because the following pong may be received prior to the
	//real pong)
	time.Sleep(200 * time.Millisecond)

	//send spoofed pong from this node id but with junk replytok
	//because the replytok will not be available to a real attacker
	//TODO- send a best reply tok guess?
	pongReq := &v4wire.Pong{
		To:         remoteEndpoint,
		ReplyTok:   make([]byte, macSize),
		Expiration: futureExpiration(),
	}
	if err := writeToPacketConn(victimConn, pongReq, remoteAddr); err != nil {
		t.Fatal(err)
	}

	//consider the target 'bonded' , as it has received the expected pong
	//send a findnode request for a random 'target' (target there being the
	//node to find)
	var fakeKey *ecdsa.PrivateKey
	if fakeKey, err = crypto.GenerateKey(); err != nil {
		t.Fatal(err)
	}
	fakePub := fakeKey.PublicKey
	lookupTarget := v4wire.EncodePubkey(&fakePub)

	findReq := &v4wire.Findnode{
		Target:     lookupTarget,
		Expiration: futureExpiration(),
	}
	if err := writeToPacketConn(victimConn, findReq, remoteAddr); err != nil {
		t.Fatal(err)
	}

	// read a pong
	readFromPacketConn(victimConn)
	// read a ping
	readFromPacketConn(victimConn)
	//if we receive a neighbours request, then the attack worked and the test should fail
	reply, err := readFromPacketConn(victimConn)
	if reply != nil && reply.Kind() == v4wire.NeighborsPacket {
		t.Error("Got neighbors")
	}
}

func FindNeighboursOnRecentlyBondedTarget(t *testing.T) {
	var err error
	var c *net.UDPConn
	var reply v4wire.Packet

	c, err = net.DialUDP("udp", nil, remoteAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	//try to bond with the target
	pingReq := v4wire.Ping{
		Version:    4,
		From:       localhostEndpoint,
		To:         remoteEndpoint,
		Expiration: futureExpiration(),
	}
	if err = sendPacket(c, &pingReq); err != nil {
		t.Fatal("First ping failed", err)
	}

	//hang around for a bit (we don't know if the target was already bonded or not)
	time.Sleep(2 * time.Second)

	//send an unsolicited neighbours packet
	var fakeKey *ecdsa.PrivateKey
	fakeKey, err = crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	fakePub := fakeKey.PublicKey
	encFakeKey := v4wire.EncodePubkey(&fakePub)
	fakeNeighbor := v4wire.Node{ID: encFakeKey, IP: net.IP{1, 2, 3, 4}, UDP: 123, TCP: 123}
	neighborsReq := v4wire.Neighbors{
		Nodes:      []v4wire.Node{fakeNeighbor},
		Expiration: futureExpiration(),
	}
	if err = sendPacket(c, &neighborsReq); err != nil {
		t.Fatal("NeighborsReq", err)
	}

	//now call find neighbours
	targetNode := enode.MustParseV4(*enodeID)
	targetEncKey := v4wire.EncodePubkey(targetNode.Pubkey())
	findReq := v4wire.Findnode{
		Target:     targetEncKey,
		Expiration: futureExpiration(),
	}
	if err = sendPacket(c, &findReq); err != nil {
		t.Fatal("sending find nodes", err)
	}
	reply, err = readPacket(c)
	if err != nil {
		t.Fatal("read find nodes", err)
	}
	if reply.Kind() != v4wire.PongPacket {
		t.Fatal("Expected pong, got", reply.Name())
	}
}

func FindNeighboursPastExpiration(t *testing.T) {
	var err error
	var c *net.UDPConn
	var reply v4wire.Packet

	c, err = net.DialUDP("udp", nil, remoteAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	//try to bond with the target
	pingReq := v4wire.Ping{
		Version:    4,
		From:       localhostEndpoint,
		To:         remoteEndpoint,
		Expiration: futureExpiration(),
	}
	if err = sendPacket(c, &pingReq); err != nil {
		t.Fatal("First ping failed", err)
	}
	// read reply to make the queue empty
	_, err = readPacket(c)
	if err != nil {
		t.Fatal("read after ping", err)
	}

	//hang around for a bit (we don't know if the target was already bonded or not)
	time.Sleep(2 * time.Second)

	//now call find neighbours
	targetNode := enode.MustParseV4(*enodeID)
	targetEncKey := v4wire.EncodePubkey(targetNode.Pubkey())
	findReq := v4wire.Findnode{
		Target:     targetEncKey,
		Expiration: -futureExpiration(),
	}
	if err = sendPacket(c, &findReq); err != nil {
		t.Fatal("sending find nodes", err)
	}
	reply, _ = readPacket(c)
	if reply.Kind() == v4wire.NeighborsPacket {
		t.Fatal("Expected no reply")
	}
}

func TestPing(t *testing.T) {
	t.Run("Ping-BasicTest(v4001)", PingKnownEnode)
	t.Run("Ping-WrongTo(v4002)", PingWrongTo)
	t.Run("Ping-WrongFrom(v4003)", PingWrongFrom)
	t.Run("Ping-ExtraData(v4004)", PingExtraData)
	t.Run("Ping-ExtraDataWrongFrom(v4005)", PingExtraDataWrongFrom)
	t.Run("Ping-PastExpiration(v4011)", PingPastExpiration)
	t.Run("Ping-WrongPacketType(v4006)", WrongPacketType)
	t.Run("Ping-BondedFromSignatureMismatch(v4009)", SourceKnownPingFromSignatureMismatch)
}

func TestSpoofing(t *testing.T) {
	t.Run("SpoofSanityCheck(v4013)", SpoofSanityCheck)
	t.Run("SpoofAmplification(v4014)", SpoofAmplificationAttackCheck)
}

func TestFindNode(t *testing.T) {
	t.Run("Findnode-UnbondedFindNeighbours(v4007)", FindNeighbours)
	t.Run("FindNode-UnsolicitedPollution(v4010)", FindNeighboursOnRecentlyBondedTarget)
	t.Run("FindNode-PastExpiration(v4012)", FindNeighboursPastExpiration)
}