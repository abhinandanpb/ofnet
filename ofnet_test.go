package ofnet

// Test ofnet APIs

import (
	"fmt"
	"net"
	"os/exec"
	"strings"
	"testing"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/contiv/ofnet/libpkt"
	"github.com/contiv/ofnet/ovsdbDriver"
	"runtime"
)

const NUM_MASTER = 2
const NUM_AGENT = 5
const NUM_ITER = 4

var vrtrMasters [NUM_MASTER]*OfnetMaster
var vxlanMasters [NUM_MASTER]*OfnetMaster
var vrtrAgents [NUM_AGENT]*OfnetAgent
var vxlanAgents [NUM_AGENT]*OfnetAgent
var ovsDrivers [NUM_AGENT * 2]*ovsdbDriver.OvsDriver

//var localIpList []string = []string{"10.10.10.1", "10.10.10.2", "10.10.10.3", "10.10.10.4"}
var localIpList []string

// Create couple of ofnet masters and few agents
func TestOfnetInit(t *testing.T) {
	var err error

	for i := 0; i < NUM_AGENT; i++ {
		localIpList = append(localIpList, fmt.Sprintf("10.10.10.%d", (i+1)))
	}

	// Create the masters
	for i := 0; i < NUM_MASTER; i++ {
		vrtrMasters[i] = NewOfnetMaster(uint16(9001 + i))
		if vrtrMasters[i] == nil {
			log.Fatalf("Error creating ofnet master")
		}

		log.Infof("Created Master: %v", vrtrMasters[i])

		vxlanMasters[i] = NewOfnetMaster(uint16(9051 + i))
		if vxlanMasters[i] == nil {
			log.Fatalf("Error creating ofnet master")
		}

		log.Infof("Created Master: %v", vxlanMasters[i])
	}

	// Wait a second for masters to be up
	time.Sleep(1 * time.Second)

	// Create agents
	for i := 0; i < NUM_AGENT; i++ {
		rpcPort := uint16(9101 + i)
		ovsPort := uint16(9151 + i)
		lclIp := net.ParseIP(localIpList[i])
		vrtrAgents[i], err = NewOfnetAgent("vrouter", lclIp, rpcPort, ovsPort)
		if err != nil {
			log.Fatalf("Error creating ofnet agent. Err: %v", err)
		}

		// Override MyAddr to local host
		vrtrAgents[i].MyAddr = "127.0.0.1"

		log.Infof("Created vrouter ofnet agent: %v", vrtrAgents[i])
	}

	for i := 0; i < NUM_AGENT; i++ {
		rpcPort := uint16(9201 + i)
		ovsPort := uint16(9251 + i)
		lclIp := net.ParseIP(localIpList[i])

		vxlanAgents[i], err = NewOfnetAgent("vxlan", lclIp, rpcPort, ovsPort)
		if err != nil {
			log.Fatalf("Error creating ofnet agent. Err: %v", err)
		}

		// Override MyAddr to local host
		vxlanAgents[i].MyAddr = "127.0.0.1"

		log.Infof("Created vxlan ofnet agent: %v", vxlanAgents[i])
	}

	// Add master node to each agent
	for i := 0; i < NUM_AGENT; i++ {
		// add the two master nodes
		for j := 0; j < NUM_MASTER; j++ {
			var resp bool
			masterInfo := OfnetNode{
				HostAddr: "127.0.0.1",
				HostPort: uint16(9001 + j),
			}
			// connect vrtr agent to vrtr master
			err := vrtrAgents[i].AddMaster(&masterInfo, &resp)
			if err != nil {
				log.Errorf("Error adding master %+v to vrtr node %d. Err: %v", masterInfo, i, err)
			}

			// connect vxlan agents to vxlan master
			masterInfo.HostPort = uint16(9051 + j)
			err = vxlanAgents[i].AddMaster(&masterInfo, &resp)
			if err != nil {
				log.Errorf("Error adding master %+v to vxlan node %d. Err: %v", masterInfo, i, err)
			}

		}
	}

	log.Infof("Ofnet masters and agents are setup..")

	time.Sleep(1 * time.Second)
	for i := 0; i < NUM_MASTER; i++ {
		err := vrtrMasters[i].MakeDummyRpcCall()
		if err != nil {
			log.Fatalf("Error making dummy rpc call. Err: %v", err)
			return
		}
		err = vxlanMasters[i].MakeDummyRpcCall()
		if err != nil {
			log.Fatalf("Error making dummy rpc call. Err: %v", err)
			return
		}
	}

	log.Infof("Made dummy rpc call to all agents")

	// Create OVS switches and connect them to vrouter ofnet agents
	for i := 0; i < NUM_AGENT; i++ {
		brName := "ovsbr1" + fmt.Sprintf("%d", i)
		ovsPort := uint16(9151 + i)
		ovsDrivers[i] = ovsdbDriver.NewOvsDriver(brName)
		err := ovsDrivers[i].AddController("127.0.0.1", ovsPort)
		if err != nil {
			t.Fatalf("Error adding controller to ovs: %s", brName)
		}
	}
	// Create OVS switches and connect them to vxlan ofnet agents
	for i := 0; i < NUM_AGENT; i++ {
		brName := "ovsbr2" + fmt.Sprintf("%d", i)
		ovsPort := uint16(9251 + i)
		j := NUM_AGENT + i
		ovsDrivers[j] = ovsdbDriver.NewOvsDriver(brName)
		err := ovsDrivers[j].AddController("127.0.0.1", ovsPort)
		if err != nil {
			t.Fatalf("Error adding controller to ovs: %s", brName)
		}
	}

	// Wait for 10sec for switch to connect to controller
	time.Sleep(10 * time.Second)
}

// test adding vlan
func TestOfnetSetupVlan(t *testing.T) {
	for i := 0; i < NUM_AGENT; i++ {
		for j := 1; j < 10; j++ {
			log.Infof("Adding Vlan %d on %s", j, localIpList[i])
			err := vrtrAgents[i].AddVlan(uint16(j), uint32(j))
			if err != nil {
				t.Errorf("Error adding vlan %d. Err: %v", j, err)
			}
			err = vxlanAgents[i].AddVlan(uint16(j), uint32(j))
			if err != nil {
				t.Errorf("Error adding vlan %d. Err: %v", j, err)
			}
		}
	}
}

// test adding full mesh vtep ports
func TestOfnetSetupVtep(t *testing.T) {
	for i := 0; i < NUM_AGENT; i++ {
		for j := 0; j < NUM_AGENT; j++ {
			if i != j {
				log.Infof("Adding VTEP on %s for remoteIp: %s", localIpList[i], localIpList[j])
				err := vrtrAgents[i].AddVtepPort(uint32(j+1), net.ParseIP(localIpList[j]))
				if err != nil {
					t.Errorf("Error adding VTEP port. Err: %v", err)
				}
				err = vxlanAgents[i].AddVtepPort(uint32(j+1), net.ParseIP(localIpList[j]))
				if err != nil {
					t.Errorf("Error adding VTEP port. Err: %v", err)
				}
			}
		}
	}

	log.Infof("Finished setting up VTEP ports..")
}

// Test adding/deleting Vrouter routes
func TestOfnetVrouteAddDelete(t *testing.T) {
	for iter := 0; iter < NUM_ITER; iter++ {
		for i := 0; i < NUM_AGENT; i++ {
			j := i + 1
			macAddr, _ := net.ParseMAC(fmt.Sprintf("02:02:02:%02x:%02x:%02x", j, j, j))
			ipAddr := net.ParseIP(fmt.Sprintf("10.10.%d.%d", j, j))
			endpoint := EndpointInfo{
				PortNo:  uint32(NUM_AGENT + 2),
				MacAddr: macAddr,
				Vlan:    1,
				IpAddr:  ipAddr,
			}

			log.Infof("Installing local vrouter endpoint: %+v", endpoint)

			// Install the local endpoint
			err := vrtrAgents[i].AddLocalEndpoint(endpoint)
			if err != nil {
				t.Fatalf("Error installing endpoint: %+v. Err: %v", endpoint, err)
				return
			}
		}

		log.Infof("Finished adding local vrouter endpoint")

		// verify all ovs switches have this route
		for i := 0; i < NUM_AGENT; i++ {
			brName := "ovsbr1" + fmt.Sprintf("%d", i)

			flowList, err := ofctlFlowDump(brName)
			if err != nil {
				t.Errorf("Error getting flow entries. Err: %v", err)
			}

			// verify flow entry exists
			for j := 0; j < NUM_AGENT; j++ {
				k := j + 1
				ipFlowMatch := fmt.Sprintf("priority=100,ip,nw_dst=10.10.%d.%d", k, k)
				ipTableId := IP_TBL_ID
				if !ofctlFlowMatch(flowList, ipTableId, ipFlowMatch) {
					t.Errorf("Could not find the route %s on ovs %s", ipFlowMatch, brName)
				}

				log.Infof("Found ipflow %s on ovs %s", ipFlowMatch, brName)
			}
		}

		log.Infof("Adding Vrouter endpoint successful.\n Testing Delete")

		for i := 0; i < NUM_AGENT; i++ {
			j := i + 1
			macAddr, _ := net.ParseMAC(fmt.Sprintf("02:02:02:%02x:%02x:%02x", j, j, j))
			ipAddr := net.ParseIP(fmt.Sprintf("10.10.%d.%d", j, j))
			endpoint := EndpointInfo{
				PortNo:  uint32(NUM_AGENT + 2),
				MacAddr: macAddr,
				Vlan:    1,
				IpAddr:  ipAddr,
			}

			log.Infof("Deleting local vrouter endpoint: %+v", endpoint)

			// Install the local endpoint
			err := vrtrAgents[i].RemoveLocalEndpoint(uint32(NUM_AGENT + 2))
			if err != nil {
				t.Fatalf("Error deleting endpoint: %+v. Err: %v", endpoint, err)
				return
			}
		}

		log.Infof("Deleted endpoints. Verifying they are gone")

		// verify flows are deleted
		for i := 0; i < NUM_AGENT; i++ {
			brName := "ovsbr1" + fmt.Sprintf("%d", i)

			flowList, err := ofctlFlowDump(brName)
			if err != nil {
				t.Errorf("Error getting flow entries. Err: %v", err)
			}

			// verify flow entry exists
			for j := 0; j < NUM_AGENT; j++ {
				k := j + 1
				ipFlowMatch := fmt.Sprintf("priority=100,ip,nw_dst=10.10.%d.%d", k, k)
				ipTableId := IP_TBL_ID
				if ofctlFlowMatch(flowList, ipTableId, ipFlowMatch) {
					t.Errorf("Still found the flow %s on ovs %s", ipFlowMatch, brName)
				}
			}
		}

		log.Infof("Verified all flows are deleted")
	}
}

// Test adding/deleting Vxlan routes
func TestOfnetVxlanAddDelete(t *testing.T) {
	for iter := 0; iter < NUM_ITER; iter++ {
		for i := 0; i < NUM_AGENT; i++ {
			j := i + 1
			macAddr, _ := net.ParseMAC(fmt.Sprintf("02:02:02:%02x:%02x:%02x", j, j, j))
			ipAddr := net.ParseIP(fmt.Sprintf("10.10.%d.%d", j, j))
			endpoint := EndpointInfo{
				PortNo:  uint32(NUM_AGENT + 2),
				MacAddr: macAddr,
				Vlan:    1,
				IpAddr:  ipAddr,
			}

			log.Infof("Installing local vxlan endpoint: %+v", endpoint)

			// Install the local endpoint
			err := vxlanAgents[i].AddLocalEndpoint(endpoint)
			if err != nil {
				t.Errorf("Error installing endpoint: %+v. Err: %v", endpoint, err)
			}
		}

		log.Infof("Finished adding local vxlan endpoint")

		// verify all ovs switches have this route
		for i := 0; i < NUM_AGENT; i++ {
			brName := "ovsbr2" + fmt.Sprintf("%d", i)

			flowList, err := ofctlFlowDump(brName)
			if err != nil {
				t.Errorf("Error getting flow entries. Err: %v", err)
			}

			// verify flow entry exists
			for j := 0; j < NUM_AGENT; j++ {
				k := j + 1
				macFlowMatch := fmt.Sprintf("priority=100,dl_vlan=1,dl_dst=02:02:02:%02x:%02x:%02x", k, k, k)

				macTableId := MAC_DEST_TBL_ID
				if !ofctlFlowMatch(flowList, macTableId, macFlowMatch) {
					t.Errorf("Could not find the mac flow %s on ovs %s", macFlowMatch, brName)
				}

				log.Infof("Found macFlow %s on ovs %s", macFlowMatch, brName)
			}
		}

		log.Infof("Add vxlan endpoint successful.\n Testing Delete")

		for i := 0; i < NUM_AGENT; i++ {
			j := i + 1
			macAddr, _ := net.ParseMAC(fmt.Sprintf("02:02:02:%02x:%02x:%02x", j, j, j))
			ipAddr := net.ParseIP(fmt.Sprintf("10.10.%d.%d", j, j))
			endpoint := EndpointInfo{
				PortNo:  uint32(NUM_AGENT + 2),
				MacAddr: macAddr,
				Vlan:    1,
				IpAddr:  ipAddr,
			}

			log.Infof("Deleting local vxlan endpoint: %+v", endpoint)

			// Install the local endpoint
			err := vxlanAgents[i].RemoveLocalEndpoint(uint32(NUM_AGENT + 2))
			if err != nil {
				t.Errorf("Error deleting endpoint: %+v. Err: %v", endpoint, err)
			}
		}

		log.Infof("Deleted endpoints. Verifying they are gone")

		// verify flow is deleted
		for i := 0; i < NUM_AGENT; i++ {
			brName := "ovsbr2" + fmt.Sprintf("%d", i)

			flowList, err := ofctlFlowDump(brName)
			if err != nil {
				t.Errorf("Error getting flow entries. Err: %v", err)
			}

			// verify flow entry exists
			for j := 0; j < NUM_AGENT; j++ {
				k := j + 1
				macFlowMatch := fmt.Sprintf("priority=100,dl_vlan=1,dl_dst=02:02:02:%02x:%02x:%02x", k, k, k)

				macTableId := MAC_DEST_TBL_ID
				if ofctlFlowMatch(flowList, macTableId, macFlowMatch) {
					t.Errorf("Still found the mac flow %s on ovs %s", macFlowMatch, brName)
				}
			}
		}
	}
}

// Wait for debug and cleanup
func TestWaitAndCleanup(t *testing.T) {
	time.Sleep(1 * time.Second)

	// Disconnect from switches.
	for i := 0; i < NUM_AGENT; i++ {
		vrtrAgents[i].Delete()
		vxlanAgents[i].Delete()
	}

	for i := 0; i < NUM_AGENT; i++ {
		brName := "ovsbr1" + fmt.Sprintf("%d", i)
		log.Infof("Deleting OVS bridge: %s", brName)
		err := ovsDrivers[i].DeleteBridge(brName)
		if err != nil {
			t.Errorf("Error deleting the bridge. Err: %v", err)
		}
	}
	for i := 0; i < NUM_AGENT; i++ {
		brName := "ovsbr2" + fmt.Sprintf("%d", i)
		log.Infof("Deleting OVS bridge: %s", brName)
		err := ovsDrivers[i].DeleteBridge(brName)
		if err != nil {
			t.Errorf("Error deleting the bridge. Err: %v", err)
		}
	}
}

// Run an ovs-ofctl command
func runOfctlCmd(cmd, brName string) ([]byte, error) {
	cmdStr := fmt.Sprintf("sudo /usr/bin/ovs-ofctl -O Openflow13 %s %s", cmd, brName)
	out, err := exec.Command("/bin/sh", "-c", cmdStr).Output()
	if err != nil {
		log.Errorf("error running ovs-ofctl %s %s. Error: %v", cmd, brName, err)
		return nil, err
	}

	return out, nil
}

// dump the flows and parse the Output
func ofctlFlowDump(brName string) ([]string, error) {
	flowDump, err := runOfctlCmd("dump-flows", brName)
	if err != nil {
		log.Errorf("Error running dump-flows on %s. Err: %v", brName, err)
		return nil, err
	}

	log.Debugf("Flow dump: %s", flowDump)
	flowOutStr := string(flowDump)
	flowDb := strings.Split(flowOutStr, "\n")[1:]

	log.Debugf("flowDb: %+v", flowDb)

	var flowList []string
	for _, flow := range flowDb {
		felem := strings.Fields(flow)
		if len(felem) > 2 {
			felem = append(felem[:1], felem[2:]...)
			felem = append(felem[:2], felem[4:]...)
			fstr := strings.Join(felem, " ")
			flowList = append(flowList, fstr)
		}
	}

	log.Debugf("flowList: %+v", flowList)

	return flowList, nil
}

// Find a flow in flow list and match its action
func ofctlFlowMatch(flowList []string, tableId int, matchStr string) bool {
	mtStr := fmt.Sprintf("table=%d, %s", tableId, matchStr)
	for _, flowEntry := range flowList {
		log.Debugf("Looking for %s in %s", mtStr, flowEntry)
		if strings.Contains(flowEntry, mtStr) {
			return true
		}
	}

	return false
}

/*
func TestCreateOvsSetup_Working(t *testing.T){

    var resp bool
  	rpcPort := uint16(9600)
  	ovsPort := uint16(9601)
  	lclIP := net.ParseIP("10.10.10.10")
		runtime.GOMAXPROCS(2)

    ofnetAgent, err := NewOfnetAgent("vrouter", lclIP, rpcPort, ovsPort)
  	if err != nil {
  		log.Fatalf("Error creating ofnet agent. Err: %v", err)
  	}

  	defer func() { ofnetAgent.Delete() }()

  	// Override MyAddr to local host
  	ofnetAgent.MyAddr = "127.0.0.1"

  	// Create a Master
  	ofnetMaster := NewOfnetMaster(uint16(9602))

  	defer func() { ofnetMaster.Delete() }()

  	masterInfo := OfnetNode{
  		HostAddr: "127.0.0.1",
  		HostPort: uint16(9602),
  	}

  	// connect vrtr agent to master
  	err = ofnetAgent.AddMaster(&masterInfo, &resp)
  	if err != nil {
  		log.Errorf("Error adding master %+v. Err: %v", masterInfo, err)
  	}

  	log.Infof("Created vrouter ofnet agent: %v", ofnetAgent)

  	brName := "ovsbr10"
  	ovsDriver := ovsdbDriver.NewOvsDriver(brName)

  	err = ovsDriver.AddController("127.0.0.1", ovsPort)
  	if err != nil {
  		log.Fatalf("Error adding controller to ovs: %s", brName)
  	}

  	defer func() { ovsDriver.DeleteBridge(brName) }()

  	// Wait for switch to connect to controller
  	time.Sleep(5 * time.Second)

  	// Create a vlan for the endpoint
  	ofnetAgent.AddVlan(11, 11)

    err = ovsDriver.CreatePort("port12", "internal", 11)
    if err != nil {
		   fmt.Printf("Error creating the port. Err: %v", err)
		   log.Errorf("Failed to create a port")
	  }
    time.Sleep(2*time.Second)
    err = ovsDriver.CreatePort("port13", "internal", 11)
    if err != nil {
		   fmt.Printf("Error creating the port. Err: %v", err)
		   log.Errorf("Failed to create a port")
	  }
    time.Sleep(5*time.Second)
		sPort,err:= ovsDriver.GetOfpPortNo("port12")
		if err!=nil {
			  fmt.Println("sPort")
        fmt.Println(err)
 	  }
      //time.Sleep(5*time.Second)
		dPort,err:= ovsDriver.GetOfpPortNo("port13")
		if err!=nil {
			 fmt.Println("dPort")
			 fmt.Println(err)
		}

    sMacAddr, _ := net.ParseMAC("02:02:02:02:02:02")
	  sIpAddr := net.ParseIP("20.20.20.20")
	  endpoint := EndpointInfo{
		  	PortNo:  sPort,
			  MacAddr: sMacAddr,
			  Vlan:    11,
			  IpAddr:  sIpAddr,
	  }
    err = ofnetAgent.AddLocalEndpoint(endpoint)
  	if err != nil {
  		log.Errorf("Error adding endpoint. Err: %v", err)
  	}
    dMacAddr, _ := net.ParseMAC("02:02:02:02:02:03")
    dIpAddr := net.ParseIP("20.20.20.30")
    endpoint = EndpointInfo{
        PortNo:  dPort,
        MacAddr: dMacAddr,
        Vlan:    11,
        IpAddr:  dIpAddr,
    }

    err = ofnetAgent.AddLocalEndpoint(endpoint)
    if err != nil {
      log.Errorf("Error adding endpoint. Err: %v", err)
    }
    ch := make(chan bool,1)
		go libpkt.VerifyPacket(ovsDriver,"port13","20.20.20.20","20.20.20.30","02:02:02:02:02:02","02:02:02:02:02:03",80,120,ch)

		log.Infof("TRYing to Send packet")

		for i:=0 ; i<5 ; i++ {
			libpkt.SendPacket(ovsDriver,"port12","20.20.20.20","20.20.20.30","02:02:02:02:02:02","02:02:02:02:02:03",80,120,11)
		}
		<-ch
		//time.Sleep(10*time.Second)
}
*/
func TestCreateOvsSetup(t *testing.T) {

	var resp bool
	var sPort uint32
	rpcPort := uint16(9600)
	ovsPort := uint16(9601)
	lclIP := net.ParseIP("10.10.10.10")
	runtime.GOMAXPROCS(2)

	ofnetAgent, err := NewOfnetAgent("vrouter", lclIP, rpcPort, ovsPort)
	if err != nil {
		log.Fatalf("Error creating ofnet agent. Err: %v", err)
	}

	//defer func() { ofnetAgent.Delete() }()

	// Override MyAddr to local host
	ofnetAgent.MyAddr = "127.0.0.1"

	// Create a Master
	ofnetMaster := NewOfnetMaster(uint16(9602))

	defer func() { ofnetMaster.Delete() }()

	masterInfo := OfnetNode{
		HostAddr: "127.0.0.1",
		HostPort: uint16(9602),
	}

	// connect vrtr agent to master
	err = ofnetAgent.AddMaster(&masterInfo, &resp)
	if err != nil {
		log.Errorf("Error adding master %+v. Err: %v", masterInfo, err)
	}

	log.Infof("Created vrouter ofnet agent: %v", ofnetAgent)

	brName := "ovsbr10"
	ovsDriver := ovsdbDriver.NewOvsDriver(brName)

	err = ovsDriver.AddController("127.0.0.1", ovsPort)
	if err != nil {
		log.Fatalf("Error adding controller to ovs: %s", brName)
	}

	//	defer func() { ovsDriver.DeleteBridge(brName) }()

	// Wait for switch to connect to controller
	time.Sleep(5 * time.Second)

	// Create a vlan for the endpoint
	ofnetAgent.AddVlan(11, 11)

	err = ovsDriver.CreatePort("port12", "internal", 11)
	if err != nil {
		fmt.Printf("Error creating the port. Err: %v", err)
		log.Errorf("Failed to create a port")
	}
	//time.Sleep(2*time.Second)
	err = ovsDriver.CreatePort("port13", "internal", 11)
	if err != nil {
		fmt.Printf("Error creating the port. Err: %v", err)
		log.Errorf("Failed to create a port")
	}
	//time.Sleep(5*time.Second)

	sPort, err = ovsDriver.GetOfpPortNo("port12")
	if err != nil {
		fmt.Println("sPort")
		fmt.Println(err)
	}
	time.Sleep(5 * time.Second)
	dPort, err := ovsDriver.GetOfpPortNo("port13")
	if err != nil {
		fmt.Println("dPort")
		fmt.Println(err)
	}

	sMacAddr, _ := net.ParseMAC("02:02:02:02:02:02")
	sIpAddr := net.ParseIP("20.20.20.20")
	endpoint := EndpointInfo{
		PortNo:  sPort,
		MacAddr: sMacAddr,
		Vlan:    11,
		IpAddr:  sIpAddr,
	}
	err = ofnetAgent.AddLocalEndpoint(endpoint)
	if err != nil {
		log.Errorf("Error adding endpoint. Err: %v", err)
	}
	dMacAddr, _ := net.ParseMAC("02:02:02:02:02:03")
	dIpAddr := net.ParseIP("20.20.20.30")
	endpoint = EndpointInfo{
		PortNo:  dPort,
		MacAddr: dMacAddr,
		Vlan:    11,
		IpAddr:  dIpAddr,
	}

	err = ofnetAgent.AddLocalEndpoint(endpoint)
	if err != nil {
		log.Errorf("Error adding endpoint. Err: %v", err)
	}
	ipv4Layer := &libpkt.IPv4Layer{
		SrcIP: "20.20.20.20",
		DstIP: "20.20.20.30",
	}
	pkt := &libpkt.Packet{SrcMac: "02:02:02:02:02:02", DstMac: "02:02:02:02:02:03", IPv4: ipv4Layer}

	//  ch := make(chan bool,1)
	go libpkt.VerifyPacket(ovsDriver, "port13", pkt)

	log.Infof("TRYing to Send packet")

	for i := 0; i < 5; i++ {
		libpkt.SendPacket(ovsDriver, "port12", pkt)
	}
	//	<-ch
	//time.Sleep(10*time.Second)
}
