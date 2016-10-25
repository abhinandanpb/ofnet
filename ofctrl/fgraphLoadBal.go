/***
Copyright 2014 Cisco Systems Inc. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package ofctrl

// This file implements the forwarding graph API for the Flood element

import (
	"fmt"
	"net"
	"unsafe"

	log "github.com/Sirupsen/logrus"
	"github.com/shaleman/libOpenflow/openflow13"
)

type LoadBal struct {
	Switch      *OFSwitch // Switch where this flood entry is present
	GroupID     uint32    // Unique id for the openflow group
	TableID     uint8     // The recirc table id
	Proto       uint16    // protool
	Zone        uint16    // vrf id (zone for conn track load balance)
	isInstalled bool      // flag to identify if group entry is installed in datapath
	Backends    []Backend //List of backend nats
}

type Backend struct {
	IP net.IP //IP of the backend
}

// Fgraph element type for the loadbal
func (self *LoadBal) Type() string {
	return "loadbal"
}

// instruction set for load balance  element
func (self *LoadBal) GetFlowInstr() openflow13.Instruction {
	if !self.isInstalled {
		return nil
	}

	groupInstr := openflow13.NewInstrApplyActions()
	groupAct := openflow13.NewActionGroup(self.GroupID)
	groupInstr.AddAction(groupAct, false)

	return groupInstr
}

//routine to install load balance group entry
func (self *LoadBal) install() error {

	groupMod := openflow13.NewGroupMod()
	groupMod.GroupId = self.GroupID

	if self.isInstalled {
		groupMod.Command = openflow13.OFPGC_MODIFY
	}

	// OF type for load balancing
	groupMod.Type = openflow13.OFPGT_SELECT

	for _, backend := range self.Backends {
		bkt := openflow13.NewBucket()
		ct := openflow13.NewActionCT(self.Zone, self.TableID)
		//ct.Length = ct.Len()
		//bkt.AddAction(ct)
		log.Infof("CT ACTION %+v ", ct)
		log.Infof("SIZE OF CT %d %d \n", ct.Len(), unsafe.Sizeof(ct))

		nat := openflow13.NewActionNat()

		nat.NatRange = openflow13.NewRange(backend.IP, net.IPv4zero, net.IPv6zero, net.IPv6zero)
		nat.Length = nat.Len()

		ct.AddAction(nat)
		log.Infof("NAT ACTION %+v ", nat)
		ct.Length = ct.Len()
		bkt.AddAction(ct)
		// Add the bucket to group
		groupMod.AddBucket(*bkt)
	}

	log.Infof("Installing Load Balancer group entry: %+v", groupMod)

	// Send it to the switch
	self.Switch.Send(groupMod)

	// Mark it as installed
	self.isInstalled = true

	return nil

}

//AddBackend - Frgaph routine to add backend to the vip
func (self *LoadBal) AddBackend(ip net.IP) error {

	self.Backends = append(self.Backends, Backend{IP: ip})

	return self.install()

}

//Frgaph routine to Remove backend to the vip
func (self *LoadBal) RemoveBackend(ip net.IP) error {

	for id, backend := range self.Backends {
		if backend.IP.Equal(ip) {
			self.Backends = append(self.Backends[:id], self.Backends[id+1:]...)
			return self.install()
		}
	}
	return fmt.Errorf("Backend does not exist for load balancer group id : %d", self.GroupID)
}

//Fgraph routine to get number of backends
func (self *LoadBal) NumBackends() int {
	return len(self.Backends)
}

func (self *LoadBal) Delete() error {
	// Remove it from OVS if its installed
	if self.isInstalled {
		groupMod := openflow13.NewGroupMod()
		groupMod.GroupId = self.GroupID
		groupMod.Command = openflow13.OFPGC_DELETE

		log.Debugf("Deleting Group entry: %+v", groupMod)

		// Send it to the switch
		self.Switch.Send(groupMod)
	}

	return nil

}
