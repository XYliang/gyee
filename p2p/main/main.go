/*
 *  Copyright (C) 2017 gyee authors
 *
 *  This file is part of the gyee library.
 *
 *  the gyee library is free software: you can redistribute it and/or modify
 *  it under the terms of the GNU General Public License as published by
 *  the Free Software Foundation, either version 3 of the License, or
 *  (at your option) any later version.
 *
 *  the gyee library is distributed in the hope that it will be useful,
 *  but WITHOUT ANY WARRANTY; without even the implied warranty of
 *  MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 *  GNU General Public License for more details.
 *
 *  You should have received a copy of the GNU General Public License
 *  along with the gyee library.  If not, see <http://www.gnu.org/licenses/>.
 *
 */


package main


import (
	"os"
	"time"
	"fmt"
	"net"
	"sync"
	"math/rand"
	"os/signal"
	golog	"log"
	shell	"github.com/yeeco/gyee/p2p/shell"
	peer	"github.com/yeeco/gyee/p2p/peer"
	config	"github.com/yeeco/gyee/p2p/config"
	log		"github.com/yeeco/gyee/p2p/logger"
	sch		"github.com/yeeco/gyee/p2p/scheduler"
)

//
// Configuration pointer
//
var p2pName2Cfg = make(map[string]*config.Config)
var p2pInst2Cfg = make(map[*sch.Scheduler]*config.Config)

//
// Indication/Package handlers
//
var (
	p2pIndHandler peer.P2pIndCallback = p2pIndProc
	p2pPkgHandler peer.P2pPkgCallback = p2pPkgProc
)

//
// extend peer identity
//
type peerIdEx struct {
	subNetId	peer.SubNetworkID
	nodeId		peer.PeerId
	dir			int
}

//
// test statistics
//
type testCaseCtrlBlock struct {
	done 	chan bool
	txSeq	int64
	rxSeq	int64
}

//
// Done signal for Tx routines
//
var doneMapLock sync.Mutex
var indCbLock sync.Mutex
var doneMap = make(map[*sch.Scheduler]map[peerIdEx]*testCaseCtrlBlock)

//
// test case
//
type testCase struct {
	name		string
	description	string
	entry		func(tc *testCase)
}

//
// test case table
//
var testCaseTable = []testCase{
	{
		name:			"testCase0",
		description:	"common case for AnySubNet",
		entry:			testCase0,
	},
	{
		name:			"testCase1",
		description:	"multiple p2p instances",
		entry:			testCase1,
	},
	{
		name:			"testCase2",
		description:	"static network without any dynamic sub networks",
		entry:			testCase2,
	},
	{
		name:			"testCase3",
		description:	"multiple sub networks without a static network",
		entry:			testCase3,
	},
	{
		name:			"testCase4",
		description:	"multiple sub networks with a static network",
		entry:			testCase4,
	},
	{
		name:			"testCase5",
		description:	"stop p2p instance",
		entry:			testCase5,
	},
}

//
// target case
//
var tgtCase = "testCase5"

//
// create test case control block by name
//
func newTcb(name string) *testCaseCtrlBlock {

	tcb := testCaseCtrlBlock {
		done:	make(chan bool, 1),
		txSeq:	0,
		rxSeq:	0,
	}

	//
	// more initialization specific to each case
	//

	switch name {
	case "testCase0":
	case "testCase1":
	case "testCase2":
	case "testCase3":
	case "testCase4":
	case "testCase5":
	default:
		log.LogCallerFileLine("newTcb: undefined test: %s", name)
		return nil
	}

	return &tcb
}

//
// Tx routine
//
func txProc(p2pInst *sch.Scheduler, dir int, snid peer.SubNetworkID, id peer.PeerId) {

	const dataTxApply = false

	//
	// This demo simply apply timer with 1s cycle and then sends a string
	// again and again; The "done" signal is also checked to determine if
	// task is done. See bellow pls.
	//

	idEx := peerIdEx {
		subNetId:	snid,
		nodeId:		id,
		dir:		dir,
	}

	doneMapLock.Lock()

	if _, exist := doneMap[p2pInst]; exist == false {
		doneMap[p2pInst] = make(map[peerIdEx] *testCaseCtrlBlock, 0)
	}

	if _, dup := doneMap[p2pInst][idEx]; dup == true {

		log.LogCallerFileLine("txProc: " +
			"duplicated, subnet: %s, id: %s",
			fmt.Sprintf("%x", snid),
			fmt.Sprintf("%X", id))

		doneMapLock.Unlock()
		return
	}

	tcb := newTcb(tgtCase)
	doneMap[p2pInst][idEx] = tcb

	doneMapLock.Unlock()

	pkg := peer.P2pPackage2Peer {
		P2pInst:		p2pInst,
		IdList: 		make([]peer.PeerId, 0),
		ProtoId:		int(peer.PID_EXT),
		PayloadLength:	0,
		Payload:		make([]byte, 0, 512),
		Extra:			nil,
	}

	log.LogCallerFileLine("txProc: " +
		"entered, subnet: %s, id: %s",
		fmt.Sprintf("%x", snid),
		fmt.Sprintf("%X", id))


	var tmHandler = func() {

		doneMapLock.Lock()

		tcb.txSeq++

		if dataTxApply {

			pkg.IdList = make([]peer.PeerId, 1)

			for id := range doneMap[p2pInst] {

				txString := fmt.Sprintf(">>>>>> \nseq:%d\n"+
					"to: subnet: %s\n, id: %s\n",
					tcb.txSeq,
					fmt.Sprintf("%x", snid),
					fmt.Sprintf("%X", id))

				pkg.SubNetId = id.subNetId
				pkg.IdList[0] = id.nodeId
				pkg.Payload = []byte(txString)
				pkg.PayloadLength = len(pkg.Payload)

				if eno := shell.P2pSendPackage(&pkg); eno != shell.P2pEnoNone {

					log.LogCallerFileLine("txProc: "+
						"send package failed, eno: %d, subnet: %s, id: %s",
						eno,
						fmt.Sprintf("%x", snid),
						fmt.Sprintf("%X", id))
				}
			}
		}

		doneMapLock.Unlock()
	}

	tm := time.NewTicker(time.Second * 1)
	defer tm.Stop()

txLoop:

	for {

		select {

		case isDone := <-tcb.done:

			if isDone {
				log.LogCallerFileLine("txProc: "+
					"it's done, isDone: %s, subnet: %s, id: %s",
					fmt.Sprintf("%t", isDone),
					fmt.Sprintf("%x", snid),
					fmt.Sprintf("%X", id))
				break txLoop
			}

		case <-tm.C:

			indCbLock.Lock()

			tmHandler()

			indCbLock.Unlock()


		default:
		}
	}

	doneMapLock.Lock()
	close(tcb.done)
	delete(doneMap[p2pInst], idEx)
	if len(doneMap[p2pInst]) == 0 {
		delete(doneMap, p2pInst)
	}
	doneMapLock.Unlock()

	log.LogCallerFileLine("txProc: " +
		"exit, subnet: %s, id: %s",
		fmt.Sprintf("%x", snid),
		fmt.Sprintf("%X", id))
}


//
// Indication handler
//
func p2pIndProc(what int, para interface{}) interface{} {

	indCbLock.Lock()
	defer indCbLock.Unlock()


	//
	// check what is indicated
	//

	switch what {

	case shell.P2pIndPeerActivated:

		//
		// a peer is activated to work, so one can install the incoming packages
		// handler.
		//

		pap := para.(*peer.P2pIndPeerActivatedPara)

		log.LogCallerFileLine("p2pIndProc: " +
			"P2pIndPeerActivated, para: %s",
			fmt.Sprintf("%+v", *pap.PeerInfo))

		if eno := shell.P2pRegisterCallback(shell.P2pPkgCb, p2pPkgHandler, pap.Ptn);
		eno != shell.P2pEnoNone {

			log.LogCallerFileLine("p2pIndProc: " +
				"P2pRegisterCallback failed, eno: %d",
				eno)
		}

		p2pInst := sch.SchGetScheduler(pap.Ptn)
		snid := pap.PeerInfo.Snid
		peerId := pap.PeerInfo.NodeId

		go txProc(p2pInst, pap.PeerInfo.Dir, snid, peerId)

	case shell.P2pIndConnStatus:

		//
		// Peer connection status report. in general, this report is resulted for
		// errors fired on the connection, one can check the "Flag" field in the
		// indication to know if p2p underlying would try to close the connection
		// itself, and one also can check the "Status" field to known what had
		// happened(the interface for this is not completed yet). Following demo
		// take a simple method: if connection is not closed by p2p itself, then
		// request p2p to close it here.
		//

		psp := para.(*peer.P2pIndConnStatusPara)
		p2pInst := sch.SchGetScheduler(psp.Ptn)

		log.LogCallerFileLine("p2pIndProc: " +
			"P2pIndConnStatus, para: %s",
			fmt.Sprintf("%+v", *psp))

		if psp.Status != 0 {

			log.LogCallerFileLine("p2pIndProc: " +
				"status: %d, close peer: %s",
				psp.Status,
				fmt.Sprintf("subnet:%x, id:%X", psp.PeerInfo.Snid, psp.PeerInfo.NodeId))

			if psp.Flag == false {

				log.LogCallerFileLine("p2pIndProc: " +
					"try to close the instance, peer: %s",
					fmt.Sprintf("subnet:%x, id:%X", psp.PeerInfo.Snid, psp.PeerInfo.NodeId))

				if eno := shell.P2pClosePeer(p2pInst, &psp.PeerInfo.Snid, &psp.PeerInfo.NodeId);
					eno != shell.P2pEnoNone {

					log.LogCallerFileLine("p2pIndProc: "+
						"P2pClosePeer failed, eno: %d, peer: %s",
						eno,
						fmt.Sprintf("subnet:%x, id:%X", psp.PeerInfo.Snid, psp.PeerInfo.NodeId))
				}
			}
		}

	case shell.P2pIndPeerClosed:

		//
		// Peer connection had been closed, one can clean his working context, see
		// bellow statements please.
		//

		pcp := para.(*peer.P2pIndPeerClosedPara)
		p2pInst := sch.SchGetScheduler(pcp.Ptn)

		log.LogCallerFileLine("p2pIndProc: " +
			"P2pIndPeerClosed, para: %s",
			fmt.Sprintf("%+v", *pcp))

		doneMapLock.Lock()
		defer doneMapLock.Unlock()

		idEx := peerIdEx{subNetId:pcp.Snid, nodeId:pcp.PeerId}
		if tcb, ok := doneMap[p2pInst][idEx]; ok && tcb != nil {
			tcb.done<-true
			break
		}

		log.LogCallerFileLine("p2pIndProc: " +
			"done failed, subnet: %s, id: %s",
			fmt.Sprintf("%x", pcp.Snid),
			fmt.Sprintf("%X", pcp.PeerId))


	default:

		log.LogCallerFileLine("p2pIndProc: " +
			"inknown indication: %d",
				what)
	}

	return para
}

//
// Package handler
//
func p2pPkgProc(pkg *peer.P2pPackage4Callback) interface{} {

	p2pInst := sch.SchGetScheduler(pkg.Ptn)
	snid := pkg.PeerInfo.Snid
	peerId := pkg.PeerInfo.NodeId

	doneMapLock.Lock()
	defer doneMapLock.Unlock()

	if _, exist := doneMap[p2pInst]; !exist {
		log.LogCallerFileLine("p2pPkgProc: " +
			"not activated, subnet: %s, id: %s",
			fmt.Sprintf("%x", snid),
			fmt.Sprintf("%X", peerId))
		return nil
	}

	idEx := peerIdEx{subNetId:snid, nodeId:peerId}
	tcb, exist := doneMap[p2pInst][idEx]
	if !exist {
		log.LogCallerFileLine("p2pPkgProc: " +
			"not activated, subnet: %s, id: %s",
			fmt.Sprintf("%x", snid),
			fmt.Sprintf("%X", peerId))
		return nil
	}

	tcb.rxSeq++

	return nil
}

//
// hook a system interrupt signal and wait on it
//
func waitInterrupt() {
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt)
	defer signal.Stop(sigc)
	<-sigc
}

//
// run target case
//
func main() {
	for _, tc := range testCaseTable {
		if tc.name == tgtCase {
			tc.entry(&tc)
			return
		}
	}
	log.LogCallerFileLine("main: target case not found: %s", tgtCase)
}

//
// testCase0
//
func testCase0(tc *testCase) {

	log.LogCallerFileLine("testCase0: going to start ycp2p ...")

	//
	// fetch default from underlying
	//

	dftCfg := shell.ShellDefaultConfig()
	if dftCfg == nil {
		log.LogCallerFileLine("testCase0: ShellDefaultConfig failed")
		return
	}

	//
	// one can then apply his configurations based on the default by calling
	// ShellSetConfig with a defferent configuration if he likes to. notice
	// that a configuration name also returned.
	//

	myCfg := *dftCfg
	cfgName := "myCfg"
	cfgName, _ = shell.ShellSetConfig(cfgName, &myCfg)
	p2pName2Cfg[cfgName] = shell.ShellGetConfig(cfgName)

	//
	// init underlying p2p logic, an instance of p2p returned
	//

	p2pInst, eno := shell.P2pCreateInstance(p2pName2Cfg[cfgName])
	if eno != sch.SchEnoNone {
		log.LogCallerFileLine("testCase0: SchSchedulerInit failed, eno: %d", eno)
		return
	}
	p2pInst2Cfg[p2pInst] = p2pName2Cfg[cfgName]

	//
	// start p2p instance
	//

	if eno = shell.P2pStart(p2pInst); eno != sch.SchEnoNone {
		log.LogCallerFileLine("testCase0: P2pStart failed, eno: %d", eno)
		return
	}

	//
	// register indication handler. notice that please, the indication handler is a
	// global object for all peers connected, while the incoming packages callback
	// handler is owned by every peer, and it can be installed while activation of
	// a peer is indicated. See demo indication handler p2pIndHandler and incoming
	// package handler p2pPkgHandler for more please.
	//

	if eno := shell.P2pRegisterCallback(shell.P2pIndCb, p2pIndHandler, p2pInst);
	eno != shell.P2pEnoNone {
		log.LogCallerFileLine("testCase0: P2pRegisterCallback failed, eno: %d", eno)
		return
	}

	log.LogCallerFileLine("testCase0: ycp2p started, cofig: %s", cfgName)

	//
	// wait os interrupt signal
	//

	waitInterrupt()
}

//
// testCase1
//
func testCase1(tc *testCase) {

	log.LogCallerFileLine("testCase1: going to start ycp2p ...")

	var p2pInstNum = 16

	var bootstrapIp net.IP
	var bootstrapId string = ""
	var bootstrapUdp uint16 = 0
	var bootstrapTcp uint16 = 0
	var bootstrapNodes = []*config.Node{}

	for loop := 0; loop < p2pInstNum; loop++ {

		cfgName := fmt.Sprintf("p2pInst%d", loop)
		log.LogCallerFileLine("testCase1: handling configuration:%s ...", cfgName)

		dftCfg := shell.ShellDefaultConfig()
		if dftCfg == nil {
			log.LogCallerFileLine("testCase1: ShellDefaultConfig failed")
			return
		}

		myCfg := *dftCfg
		myCfg.Name = cfgName
		myCfg.Local.IP = net.IP{127, 0, 0, 1}
		myCfg.Local.UDP = uint16(30303 + loop)
		myCfg.Local.TCP = uint16(30303 + loop)

		if loop == 0 {
			myCfg.NoDial = true
			myCfg.BootstrapNode = true
		}

		myCfg.BootstrapNodes = nil
		if loop != 0 {
			myCfg.BootstrapNodes = append(myCfg.BootstrapNodes, bootstrapNodes...)
		}

		cfgName, _ = shell.ShellSetConfig(cfgName, &myCfg)
		p2pName2Cfg[cfgName] = shell.ShellGetConfig(cfgName)

		if loop == 0 {
			bootstrapIp = p2pName2Cfg[cfgName].Local.IP
			bootstrapId = fmt.Sprintf("%X", p2pName2Cfg[cfgName].Local.ID)
			bootstrapUdp = p2pName2Cfg[cfgName].Local.UDP
			bootstrapTcp = p2pName2Cfg[cfgName].Local.TCP

			ipv4 := bootstrapIp.To4()
			url := []string {
				fmt.Sprintf("%s@%d.%d.%d.%d:%d:%d",
					bootstrapId,
					ipv4[0],ipv4[1],ipv4[2],ipv4[3],
					bootstrapUdp,
					bootstrapTcp),
			}
			bootstrapNodes = append(bootstrapNodes,config.P2pSetupBootstrapNodes(url)...)
		}

		p2pInst, eno := shell.P2pCreateInstance(p2pName2Cfg[cfgName])
		if eno != sch.SchEnoNone {
			log.LogCallerFileLine("testCase1: SchSchedulerInit failed, eno: %d", eno)
			return
		}
		p2pInst2Cfg[p2pInst] = p2pName2Cfg[cfgName]

		if eno = shell.P2pStart(p2pInst); eno != sch.SchEnoNone {
			log.LogCallerFileLine("testCase1: P2pStart failed, eno: %d", eno)
			return
		}

		if eno := shell.P2pRegisterCallback(shell.P2pIndCb, p2pIndHandler, p2pInst);
			eno != shell.P2pEnoNone {
			log.LogCallerFileLine("testCase1: P2pRegisterCallback failed, eno: %d", eno)
			return
		}

		log.LogCallerFileLine("testCase1: ycp2p started, cofig: %s", cfgName)
	}

	waitInterrupt()
}

//
// testCase2
//
func testCase2(tc *testCase) {

	log.LogCallerFileLine("testCase2: going to start ycp2p ...")

	var p2pInstNum = 8
	var cfgName = ""

	var staticNodeIdList = []*config.Node{}

	for loop := 0; loop < p2pInstNum; loop++ {

		cfgName = fmt.Sprintf("p2pInst%d", loop)
		log.LogCallerFileLine("testCase2: prepare node identity: %s ...", cfgName)

		dftCfg := shell.ShellDefaultConfig()
		if dftCfg == nil {
			log.LogCallerFileLine("testCase2: ShellDefaultConfig failed")
			return
		}

		myCfg := *dftCfg
		myCfg.Name = cfgName
		myCfg.PrivateKey = nil
		myCfg.PublicKey = nil

		if config.P2pSetupLocalNodeId(&myCfg) != config.PcfgEnoNone {
			log.LogCallerFileLine("testCase2: P2pSetupLocalNodeId failed")
			return
		}

		log.LogCallerFileLine("testCase2: cfgName: %s, id: %X",
			cfgName, myCfg.Local.ID)

		n := config.Node{
			IP:		net.IP{127, 0, 0, 1},
			UDP:	uint16(30303 + loop),
			TCP:	uint16(30303 + loop),
			ID:		myCfg.Local.ID,
		}

		staticNodeIdList = append(staticNodeIdList, &n)
	}

	for loop := 0; loop < p2pInstNum; loop++ {

		cfgName := fmt.Sprintf("p2pInst%d", loop)
		log.LogCallerFileLine("testCase2: handling configuration:%s ...", cfgName)

		dftCfg := shell.ShellDefaultConfig()
		if dftCfg == nil {
			log.LogCallerFileLine("testCase2: ShellDefaultConfig failed")
			return
		}

		myCfg := *dftCfg
		myCfg.Name = cfgName
		myCfg.PrivateKey = nil
		myCfg.PublicKey = nil
		myCfg.NetworkType = config.P2pNetworkTypeStatic
		myCfg.StaticNetId = config.ZeroSubNet
		myCfg.Local = *staticNodeIdList[loop]

		for idx, n := range staticNodeIdList {
			if idx != loop {
				myCfg.StaticNodes = append(myCfg.StaticNodes, n)
			}
		}

		myCfg.StaticMaxPeers = len(myCfg.StaticNodes) * 2
		myCfg.StaticMaxOutbounds = len(myCfg.StaticNodes)
		myCfg.StaticMaxInbounds = len(myCfg.StaticNodes)

		cfgName, _ = shell.ShellSetConfig(cfgName, &myCfg)
		p2pName2Cfg[cfgName] = shell.ShellGetConfig(cfgName)

		p2pInst, eno := shell.P2pCreateInstance(p2pName2Cfg[cfgName])
		if eno != sch.SchEnoNone {
			log.LogCallerFileLine("testCase2: SchSchedulerInit failed, eno: %d", eno)
			return
		}

		p2pInst2Cfg[p2pInst] = p2pName2Cfg[cfgName]
	}

	var p2pInstList = []*sch.Scheduler{}
	for p2pInst := range p2pInst2Cfg {
		p2pInstList = append(p2pInstList, p2pInst)
	}

	for piNum := len(p2pInstList); piNum > 0; piNum-- {

		time.Sleep(time.Second * 2)

		pidx := rand.Intn(piNum)
		p2pInst := p2pInstList[pidx]
		p2pInstList = append(p2pInstList[0:pidx], p2pInstList[pidx+1:]...)

		if eno := shell.P2pStart(p2pInst); eno != sch.SchEnoNone {
			log.LogCallerFileLine("testCase2: P2pStart failed, eno: %d", eno)
			return
		}

		if eno := shell.P2pRegisterCallback(shell.P2pIndCb, p2pIndHandler, p2pInst);
			eno != shell.P2pEnoNone {
			log.LogCallerFileLine("testCase2: P2pRegisterCallback failed, eno: %d", eno)
			return
		}

		log.LogCallerFileLine("testCase2: ycp2p started, cofig: %s", cfgName)
	}

	waitInterrupt()
}

//
// testCase3
//
func testCase3(tc *testCase) {

	log.LogCallerFileLine("testCase3: going to start ycp2p ...")

	var p2pInstNum = 16

	var bootstrapIp net.IP
	var bootstrapId string = ""
	var bootstrapUdp uint16 = 0
	var bootstrapTcp uint16 = 0
	var bootstrapNodes = []*config.Node{}

	for loop := 0; loop < p2pInstNum; loop++ {

		cfgName := fmt.Sprintf("p2pInst%d", loop)
		log.LogCallerFileLine("testCase3: handling configuration:%s ...", cfgName)

		var dftCfg *config.Config = nil

		if loop == 0 {
			if dftCfg = shell.ShellDefaultBootstrapConfig(); dftCfg == nil {
				log.LogCallerFileLine("testCase3: ShellDefaultBootstrapConfig failed")
				return
			}
		} else {
			if dftCfg = shell.ShellDefaultConfig(); dftCfg == nil {
				log.LogCallerFileLine("testCase3: ShellDefaultConfig failed")
				return
			}
		}

		myCfg := *dftCfg
		myCfg.Name = cfgName
		myCfg.PrivateKey = nil
		myCfg.PublicKey = nil
		myCfg.NetworkType = config.P2pNetworkTypeDynamic
		myCfg.Local.IP = net.IP{127, 0, 0, 1}
		myCfg.Local.UDP = uint16(30303 + loop)
		myCfg.Local.TCP = uint16(30303 + loop)

		if loop == 0 {
			for idx := 0; idx < p2pInstNum; idx++ {
				snid0 := config.SubNetworkID{0xff, byte(idx & 0x0f)}
				myCfg.SubNetIdList = append(myCfg.SubNetIdList, snid0)
				myCfg.SubNetMaxPeers[snid0] = config.MaxPeers
				myCfg.SubNetMaxInBounds[snid0] = config.MaxPeers
				myCfg.SubNetMaxOutbounds[snid0] = 0
			}
		} else {
			snid0 := config.SubNetworkID{0xff, byte(loop & 0x0f)}
			snid1 := config.SubNetworkID{0xff, byte((loop + 1) & 0x0f)}
			myCfg.SubNetIdList = append(myCfg.SubNetIdList, snid0)
			myCfg.SubNetIdList = append(myCfg.SubNetIdList, snid1)
			myCfg.SubNetMaxPeers[snid0] = config.MaxPeers
			myCfg.SubNetMaxInBounds[snid0] = config.MaxInbounds
			myCfg.SubNetMaxOutbounds[snid0] = config.MaxOutbounds
			myCfg.SubNetMaxPeers[snid1] = config.MaxPeers
			myCfg.SubNetMaxInBounds[snid1] = config.MaxInbounds
			myCfg.SubNetMaxOutbounds[snid1] = config.MaxOutbounds
		}

		if loop == 0 {
			myCfg.NoDial = true
			myCfg.NoAccept = true
			myCfg.BootstrapNode = true
		} else {
			myCfg.NoDial = false
			myCfg.NoAccept = false
			myCfg.BootstrapNode = false
		}

		myCfg.BootstrapNodes = nil
		if loop != 0 {
			myCfg.BootstrapNodes = append(myCfg.BootstrapNodes, bootstrapNodes...)
		}

		cfgName, _ = shell.ShellSetConfig(cfgName, &myCfg)
		p2pName2Cfg[cfgName] = shell.ShellGetConfig(cfgName)

		if loop == 0 {

			bootstrapIp = p2pName2Cfg[cfgName].Local.IP
			bootstrapId = fmt.Sprintf("%X", p2pName2Cfg[cfgName].Local.ID)
			bootstrapUdp = p2pName2Cfg[cfgName].Local.UDP
			bootstrapTcp = p2pName2Cfg[cfgName].Local.TCP

			ipv4 := bootstrapIp.To4()
			url := []string {
				fmt.Sprintf("%s@%d.%d.%d.%d:%d:%d",
					bootstrapId,
					ipv4[0],ipv4[1],ipv4[2],ipv4[3],
					bootstrapUdp,
					bootstrapTcp),
			}
			bootstrapNodes = append(bootstrapNodes,config.P2pSetupBootstrapNodes(url)...)
		}

		p2pInst, eno := shell.P2pCreateInstance(p2pName2Cfg[cfgName])
		if eno != sch.SchEnoNone {
			log.LogCallerFileLine("testCase3: SchSchedulerInit failed, eno: %d", eno)
			return
		}
		p2pInst2Cfg[p2pInst] = p2pName2Cfg[cfgName]

		if eno = shell.P2pStart(p2pInst); eno != sch.SchEnoNone {
			log.LogCallerFileLine("testCase3: P2pStart failed, eno: %d", eno)
			return
		}

		if eno := shell.P2pRegisterCallback(shell.P2pIndCb, p2pIndHandler, p2pInst);
			eno != shell.P2pEnoNone {
			log.LogCallerFileLine("testCase3: P2pRegisterCallback failed, eno: %d", eno)
			return
		}

		log.LogCallerFileLine("testCase3: ycp2p started, cofig: %s", cfgName)
	}

	waitInterrupt()
}

//
// testCase4
//
func testCase4(tc *testCase) {

	log.LogCallerFileLine("testCase4: going to start ycp2p ...")

	var p2pInstNum = 8

	var bootstrapIp net.IP
	var bootstrapId string = ""
	var bootstrapUdp uint16 = 0
	var bootstrapTcp uint16 = 0
	var bootstrapNodes = []*config.Node{}
	var p2pInstBootstrap *sch.Scheduler = nil

	var staticNodeIdList = []*config.Node{}

	for loop := 0; loop < p2pInstNum; loop++ {

		cfgName := fmt.Sprintf("p2pInst%d", loop)
		log.LogCallerFileLine("testCase4: prepare node identity: %s ...", cfgName)

		dftCfg := shell.ShellDefaultConfig()
		if dftCfg == nil {
			log.LogCallerFileLine("testCase4: ShellDefaultConfig failed")
			return
		}

		myCfg := *dftCfg
		myCfg.Name = cfgName
		myCfg.PrivateKey = nil
		myCfg.PublicKey = nil

		if config.P2pSetupLocalNodeId(&myCfg) != config.PcfgEnoNone {
			log.LogCallerFileLine("testCase4: P2pSetupLocalNodeId failed")
			return
		}

		log.LogCallerFileLine("testCase4: cfgName: %s, id: %X",
			cfgName, myCfg.Local.ID)

		n := config.Node{
			IP:		net.IP{127, 0, 0, 1},
			UDP:	uint16(30303 + loop),
			TCP:	uint16(30303 + loop),
			ID:		myCfg.Local.ID,
		}

		staticNodeIdList = append(staticNodeIdList, &n)
	}

	for loop := 0; loop < p2pInstNum; loop++ {

		cfgName := fmt.Sprintf("p2pInst%d", loop)
		log.LogCallerFileLine("testCase4: handling configuration:%s ...", cfgName)

		var dftCfg *config.Config = nil

		if loop == 0 {
			if dftCfg = shell.ShellDefaultBootstrapConfig(); dftCfg == nil {
				log.LogCallerFileLine("testCase4: ShellDefaultBootstrapConfig failed")
				return
			}
		} else {
			if dftCfg = shell.ShellDefaultConfig(); dftCfg == nil {
				log.LogCallerFileLine("testCase4: ShellDefaultConfig failed")
				return
			}
		}

		myCfg := *dftCfg
		myCfg.Name = cfgName
		myCfg.PrivateKey = nil
		myCfg.PublicKey = nil
		myCfg.NetworkType = config.P2pNetworkTypeDynamic
		myCfg.StaticNetId = config.ZeroSubNet
		myCfg.Local.IP = net.IP{127, 0, 0, 1}
		myCfg.Local.UDP = uint16(30303 + loop)
		myCfg.Local.TCP = uint16(30303 + loop)
		myCfg.Local.ID = (*staticNodeIdList[loop]).ID

		for idx, n := range staticNodeIdList {
			if idx != loop {
				myCfg.StaticNodes = append(myCfg.StaticNodes, n)
			}
		}

		myCfg.StaticMaxPeers = len(myCfg.StaticNodes) * 2	// config.MaxPeers
		myCfg.StaticMaxOutbounds = len(myCfg.StaticNodes)	// config.MaxOutbounds
		myCfg.StaticMaxInbounds = len(myCfg.StaticNodes) 	// config.MaxInbounds

		if loop == 0 {
			for idx := 0; idx < p2pInstNum; idx++ {
				snid0 := config.SubNetworkID{0xff, byte(idx & 0x0f)}
				myCfg.SubNetIdList = append(myCfg.SubNetIdList, snid0)
				myCfg.SubNetMaxPeers[snid0] = config.MaxPeers
				myCfg.SubNetMaxInBounds[snid0] = config.MaxPeers
				myCfg.SubNetMaxOutbounds[snid0] = 0
			}
		} else {
			snid0 := config.SubNetworkID{0xff, byte(loop & 0x0f)}
			snid1 := config.SubNetworkID{0xff, byte((loop + 1) & 0x0f)}
			myCfg.SubNetIdList = append(myCfg.SubNetIdList, snid0)
			myCfg.SubNetIdList = append(myCfg.SubNetIdList, snid1)
			myCfg.SubNetMaxPeers[snid0] = config.MaxPeers
			myCfg.SubNetMaxInBounds[snid0] = config.MaxInbounds
			myCfg.SubNetMaxOutbounds[snid0] = config.MaxOutbounds
			myCfg.SubNetMaxPeers[snid1] = config.MaxPeers
			myCfg.SubNetMaxInBounds[snid1] = config.MaxInbounds
			myCfg.SubNetMaxOutbounds[snid1] = config.MaxOutbounds
		}

		if loop == 0 {
			myCfg.NoDial = true
			myCfg.NoAccept = true
			myCfg.BootstrapNode = true
		} else {
			myCfg.NoDial = false
			myCfg.NoAccept = false
			myCfg.BootstrapNode = false
		}

		myCfg.BootstrapNodes = nil
		if loop != 0 {
			myCfg.BootstrapNodes = append(myCfg.BootstrapNodes, bootstrapNodes...)
		}

		cfgName, _ = shell.ShellSetConfig(cfgName, &myCfg)
		p2pName2Cfg[cfgName] = shell.ShellGetConfig(cfgName)

		if loop == 0 {

			bootstrapIp = append(bootstrapIp, p2pName2Cfg[cfgName].Local.IP[:]...)
			bootstrapId = fmt.Sprintf("%X", p2pName2Cfg[cfgName].Local.ID)
			bootstrapUdp = p2pName2Cfg[cfgName].Local.UDP
			bootstrapTcp = p2pName2Cfg[cfgName].Local.TCP

			ipv4 := bootstrapIp.To4()
			url := []string{
				fmt.Sprintf("%s@%d.%d.%d.%d:%d:%d",
					bootstrapId,
					ipv4[0], ipv4[1], ipv4[2], ipv4[3],
					bootstrapUdp,
					bootstrapTcp),
			}
			bootstrapNodes = append(bootstrapNodes, config.P2pSetupBootstrapNodes(url)...)
		}

		p2pInst, eno := shell.P2pCreateInstance(p2pName2Cfg[cfgName])
		if eno != sch.SchEnoNone {
			log.LogCallerFileLine("testCase4: SchSchedulerInit failed, eno: %d", eno)
			return
		}
		p2pInst2Cfg[p2pInst] = p2pName2Cfg[cfgName]

		if loop == 0 {
			p2pInstBootstrap = p2pInst
		}
	}

	var p2pInstList = []*sch.Scheduler{}
	for p2pInst := range p2pInst2Cfg {
		if p2pInst != p2pInstBootstrap {
			p2pInstList = append(p2pInstList, p2pInst)
		}
	}

	if eno := shell.P2pStart(p2pInstBootstrap); eno != sch.SchEnoNone {
		log.LogCallerFileLine("testCase4: P2pStart failed, eno: %d", eno)
		return
	}

	for piNum := len(p2pInstList); piNum > 0; piNum-- {

		time.Sleep(time.Second * 2)

		pidx := rand.Intn(piNum)
		p2pInst := p2pInstList[pidx]
		p2pInstList = append(p2pInstList[0:pidx], p2pInstList[pidx+1:]...)

		if eno := shell.P2pStart(p2pInst); eno != sch.SchEnoNone {
			log.LogCallerFileLine("testCase4: P2pStart failed, eno: %d", eno)
			return
		}

		if eno := shell.P2pRegisterCallback(shell.P2pIndCb, p2pIndHandler, p2pInst);
			eno != shell.P2pEnoNone {
			log.LogCallerFileLine("testCase4: P2pRegisterCallback failed, eno: %d", eno)
			return
		}

		cfgName := p2pInst.SchGetP2pCfgName()
		log.LogCallerFileLine("testCase4: ycp2p started, cofig: %s", cfgName)
	}

	waitInterrupt()
}

//
// testCase5
//
func testCase5(tc *testCase) {

	log.LogCallerFileLine("testCase5: going to start ycp2p ...")

	var p2pInstNum = 8

	var bootstrapIp net.IP
	var bootstrapId string = ""
	var bootstrapUdp uint16 = 0
	var bootstrapTcp uint16 = 0
	var bootstrapNodes = []*config.Node{}
	var p2pInstBootstrap *sch.Scheduler = nil

	var staticNodeIdList = []*config.Node{}

	for loop := 0; loop < p2pInstNum; loop++ {

		cfgName := fmt.Sprintf("p2pInst%d", loop)
		log.LogCallerFileLine("testCase5: prepare node identity: %s ...", cfgName)

		dftCfg := shell.ShellDefaultConfig()
		if dftCfg == nil {
			log.LogCallerFileLine("testCase5: ShellDefaultConfig failed")
			return
		}

		myCfg := *dftCfg
		myCfg.Name = cfgName
		myCfg.PrivateKey = nil
		myCfg.PublicKey = nil

		if config.P2pSetupLocalNodeId(&myCfg) != config.PcfgEnoNone {
			log.LogCallerFileLine("testCase5: P2pSetupLocalNodeId failed")
			return
		}

		log.LogCallerFileLine("testCase5: cfgName: %s, id: %X",
			cfgName, myCfg.Local.ID)

		n := config.Node{
			IP:		net.IP{127, 0, 0, 1},
			UDP:	uint16(30303 + loop),
			TCP:	uint16(30303 + loop),
			ID:		myCfg.Local.ID,
		}

		staticNodeIdList = append(staticNodeIdList, &n)
	}

	for loop := 0; loop < p2pInstNum; loop++ {

		cfgName := fmt.Sprintf("p2pInst%d", loop)
		log.LogCallerFileLine("testCase5: handling configuration:%s ...", cfgName)

		var dftCfg *config.Config = nil

		if loop == 0 {
			if dftCfg = shell.ShellDefaultBootstrapConfig(); dftCfg == nil {
				log.LogCallerFileLine("testCase5: ShellDefaultBootstrapConfig failed")
				return
			}
		} else {
			if dftCfg = shell.ShellDefaultConfig(); dftCfg == nil {
				log.LogCallerFileLine("testCase5: ShellDefaultConfig failed")
				return
			}
		}

		myCfg := *dftCfg
		myCfg.Name = cfgName
		myCfg.PrivateKey = nil
		myCfg.PublicKey = nil
		myCfg.NetworkType = config.P2pNetworkTypeDynamic
		myCfg.StaticNetId = config.ZeroSubNet
		myCfg.Local.IP = net.IP{127, 0, 0, 1}
		myCfg.Local.UDP = uint16(30303 + loop)
		myCfg.Local.TCP = uint16(30303 + loop)
		myCfg.Local.ID = (*staticNodeIdList[loop]).ID

		for idx, n := range staticNodeIdList {
			if idx != loop {
				myCfg.StaticNodes = append(myCfg.StaticNodes, n)
			}
		}

		myCfg.StaticMaxPeers = len(myCfg.StaticNodes) * 2	// config.MaxPeers
		myCfg.StaticMaxOutbounds = len(myCfg.StaticNodes)	// config.MaxOutbounds
		myCfg.StaticMaxInbounds = len(myCfg.StaticNodes) 	// config.MaxInbounds

		if loop == 0 {
			for idx := 0; idx < p2pInstNum; idx++ {
				snid0 := config.SubNetworkID{0xff, byte(idx & 0x0f)}
				myCfg.SubNetIdList = append(myCfg.SubNetIdList, snid0)
				myCfg.SubNetMaxPeers[snid0] = config.MaxPeers
				myCfg.SubNetMaxInBounds[snid0] = config.MaxPeers
				myCfg.SubNetMaxOutbounds[snid0] = 0
			}
		} else {
			snid0 := config.SubNetworkID{0xff, byte(loop & 0x0f)}
			snid1 := config.SubNetworkID{0xff, byte((loop + 1) & 0x0f)}
			myCfg.SubNetIdList = append(myCfg.SubNetIdList, snid0)
			myCfg.SubNetIdList = append(myCfg.SubNetIdList, snid1)
			myCfg.SubNetMaxPeers[snid0] = config.MaxPeers
			myCfg.SubNetMaxInBounds[snid0] = config.MaxInbounds
			myCfg.SubNetMaxOutbounds[snid0] = config.MaxOutbounds
			myCfg.SubNetMaxPeers[snid1] = config.MaxPeers
			myCfg.SubNetMaxInBounds[snid1] = config.MaxInbounds
			myCfg.SubNetMaxOutbounds[snid1] = config.MaxOutbounds
		}

		if loop == 0 {
			myCfg.NoDial = true
			myCfg.NoAccept = true
			myCfg.BootstrapNode = true
		} else {
			myCfg.NoDial = false
			myCfg.NoAccept = false
			myCfg.BootstrapNode = false
		}

		myCfg.BootstrapNodes = nil
		if loop != 0 {
			myCfg.BootstrapNodes = append(myCfg.BootstrapNodes, bootstrapNodes...)
		}

		cfgName, _ = shell.ShellSetConfig(cfgName, &myCfg)
		p2pName2Cfg[cfgName] = shell.ShellGetConfig(cfgName)

		if loop == 0 {

			bootstrapIp = append(bootstrapIp, p2pName2Cfg[cfgName].Local.IP[:]...)
			bootstrapId = fmt.Sprintf("%X", p2pName2Cfg[cfgName].Local.ID)
			bootstrapUdp = p2pName2Cfg[cfgName].Local.UDP
			bootstrapTcp = p2pName2Cfg[cfgName].Local.TCP

			ipv4 := bootstrapIp.To4()
			url := []string{
				fmt.Sprintf("%s@%d.%d.%d.%d:%d:%d",
					bootstrapId,
					ipv4[0], ipv4[1], ipv4[2], ipv4[3],
					bootstrapUdp,
					bootstrapTcp),
			}
			bootstrapNodes = append(bootstrapNodes, config.P2pSetupBootstrapNodes(url)...)
		}

		p2pInst, eno := shell.P2pCreateInstance(p2pName2Cfg[cfgName])
		if eno != sch.SchEnoNone {
			log.LogCallerFileLine("testCase5: SchSchedulerInit failed, eno: %d", eno)
			return
		}
		p2pInst2Cfg[p2pInst] = p2pName2Cfg[cfgName]

		if loop == 0 {
			p2pInstBootstrap = p2pInst
		}
	}

	var p2pInstList = []*sch.Scheduler{}
	for p2pInst := range p2pInst2Cfg {
		if p2pInst != p2pInstBootstrap {
			p2pInstList = append(p2pInstList, p2pInst)
		}
	}

	if eno := shell.P2pStart(p2pInstBootstrap); eno != sch.SchEnoNone {
		log.LogCallerFileLine("testCase5: P2pStart failed, eno: %d", eno)
		return
	}

	for piNum := len(p2pInstList); piNum > 0; piNum-- {

		time.Sleep(time.Second * 2)

		pidx := rand.Intn(piNum)
		p2pInst := p2pInstList[pidx]
		p2pInstList = append(p2pInstList[0:pidx], p2pInstList[pidx+1:]...)

		if eno := shell.P2pStart(p2pInst); eno != sch.SchEnoNone {
			log.LogCallerFileLine("testCase5: P2pStart failed, eno: %d", eno)
			return
		}

		if eno := shell.P2pRegisterCallback(shell.P2pIndCb, p2pIndHandler, p2pInst);
			eno != shell.P2pEnoNone {
			log.LogCallerFileLine("testCase5: P2pRegisterCallback failed, eno: %d", eno)
			return
		}

		cfgName := p2pInst.SchGetP2pCfgName()
		log.LogCallerFileLine("testCase5: ycp2p started, cofig: %s", cfgName)
	}

	//
	// Sleep and then stop p2p instance
	//

	time.Sleep(time.Second * 2)
	golog.Printf("testCase5: going to stop p2p instances ...")

	for p2pInst, _ := range p2pInst2Cfg {
		if eno := shell.P2pStop(p2pInst); eno != sch.SchEnoNone {
			golog.Printf("testCase5: " +
				"P2pStop failed, instance: %s",
				p2pInst.SchGetP2pCfgName())
		}
	}

	golog.Printf("testCase5: it's the end")


	waitInterrupt()
}

