package main

import (
	"log"
	"net"

	"github.com/mingzhaodotname/dhcp4client"
	"time"
	dhcp "github.com/krolaw/dhcp4"
	"math/rand"
	//"strconv"
	//"github.com/krolaw/dhcp4/conn"
	"flag"
)

func main() {
	log.Println("this is main")
	cidr := flag.String("cidr", "", "CIDR of an interface, e.g. 192.168.1.3/24")
	mac := flag.String("mac", "", "MAC address")
	flag.Parse()
	log.Println("cidr: ", *cidr)
	ip, ipnet, err := net.ParseCIDR(*cidr)
	if err != nil {
		log.Fatal("error: ", err)
	}
	log.Println("ip: ", ip, ", mask: ", ipnet.Mask, ", ipnet.ip: ", ipnet.IP)
	ipnet.Mask[0] = 255 ^ ipnet.Mask[0]
	ipnet.Mask[1] = 255 ^ ipnet.Mask[1]
	ipnet.Mask[2] = 255 ^ ipnet.Mask[2]
	ipnet.Mask[3] = 255 ^ ipnet.Mask[3]

	ip[12] = ipnet.Mask[0] | ip[12]
	ip[13] = ipnet.Mask[1] | ip[13]
	ip[14] = ipnet.Mask[2] | ip[14]
	ip[15] = ipnet.Mask[3] | ip[15]
	log.Println("ip: ", ip, ", mask: ", ipnet.Mask, ", ipnet.ip: ", ipnet.IP)

	//ipnet.Mask

	log.Println("mac: ", *mac)
	//go SendDiscovery()
	ExampleHandler(ip, *mac)

	//go ExampleHandler()
	//SendDiscovery()
}

func ListenAndServe(handler dhcp.Handler, ip net.IP, mac string) error {
	l, err := net.ListenPacket("udp4", ":68")
	//conn, err := net.ListenUDP("udp4", &c.laddr)
	if err != nil {
		return err
	}
	defer l.Close()
	log.Println("l.LocalAddr(): ", l.LocalAddr())

	// Write DHCP request packet
	log.Println("sending discovery packet")
	dp := dhcp4client.DiscoverPacket(mac);
	//addr := &net.UDPAddr{IP: net.IPv4(192, 168, 1, 255), Port: 67}
	addr := &net.UDPAddr{IP: ip, Port: 67}

	if _, e := l.WriteTo(dp, addr); e != nil {
		return e
	}
	log.Println("sent discovery packet successfully.")

	return dhcp.Serve(l, handler)
}

// Example using DHCP with a single network interface device
func ExampleHandler(ip net.IP, mac string) {
	log.Println("minglog: started ExampleHandler")
	// serverIP := net.IP{10, 0, 2, 15}
	serverIP := net.IP{192, 168, 1, 3}
	handler := &DHCPHandler{
		ip:            serverIP,
		leaseDuration: 2 * time.Hour,
		// start:         net.IP{10, 0, 2, 15},
		start:         net.IP{192, 168, 1, 3},
		leaseRange:    50,
		leases:        make(map[int]lease, 10),
		options: dhcp.Options{
			dhcp.OptionSubnetMask:       []byte{255, 255, 255, 0},
			dhcp.OptionRouter:           []byte(serverIP), // Presuming Server is also your router
			dhcp.OptionDomainNameServer: []byte(serverIP), // Presuming Server is also your DNS server
		},
	}
	log.Fatal(ListenAndServe(handler, ip, mac))
	// log.Fatal(dhcp.Serve(dhcp.NewUDP4BoundListener("eth0",":67"), handler)) // Select interface on multi interface device - just linux for now
	// log.Fatal(dhcp.Serve(dhcp.NewUDP4FilterListener("en0",":67"), handler)) // Work around for other OSes
}

type lease struct {
	nic    string    // Client's CHAddr
	expiry time.Time // When the lease expires
}

type DHCPHandler struct {
	ip            net.IP        // Server IP to use
	options       dhcp.Options  // Options to send to DHCP Clients
	start         net.IP        // Start of IP range to distribute
	leaseRange    int           // Number of IPs to distribute (starting from start)
	leaseDuration time.Duration // Lease period
	leases        map[int]lease // Map to keep track of leases
}

func (h *DHCPHandler) ServeDHCP(p dhcp.Packet, msgType dhcp.MessageType, options dhcp.Options) (d dhcp.Packet) {
	switch msgType {

	case dhcp.Offer:
		log.Println("=== minglog: dhcp Offer", p, options)

	case dhcp.Discover:
		log.Println("=== minglog: dhcp Discover", p, options)
		return nil

		free, nic := -1, p.CHAddr().String()
		for i, v := range h.leases { // Find previous lease
			if v.nic == nic {
				free = i
				goto reply
			}
		}
		if free = h.freeLease(); free == -1 {
			log.Println("=== minglog: dhcp Discover - no free lease")
			return
		}
		reply:
		log.Println("=== minglog: dhcp Discover, free:", free)

		return dhcp.ReplyPacket(p, dhcp.Offer, h.ip, dhcp.IPAdd(h.start, free), h.leaseDuration,
			h.options.SelectOrderOrAll(options[dhcp.OptionParameterRequestList]))

	case dhcp.Request:
		log.Println("=== minglog: dhcp Request")
		if server, ok := options[dhcp.OptionServerIdentifier]; ok && !net.IP(server).Equal(h.ip) {
			return nil // Message not for this dhcp server
		}
		reqIP := net.IP(options[dhcp.OptionRequestedIPAddress])
		if reqIP == nil {
			reqIP = net.IP(p.CIAddr())
		}

		if len(reqIP) == 4 && !reqIP.Equal(net.IPv4zero) {
			if leaseNum := dhcp.IPRange(h.start, reqIP) - 1; leaseNum >= 0 && leaseNum < h.leaseRange {
				if l, exists := h.leases[leaseNum]; !exists || l.nic == p.CHAddr().String() {
					h.leases[leaseNum] = lease{nic: p.CHAddr().String(), expiry: time.Now().Add(h.leaseDuration)}
					return dhcp.ReplyPacket(p, dhcp.ACK, h.ip, reqIP, h.leaseDuration,
						h.options.SelectOrderOrAll(options[dhcp.OptionParameterRequestList]))
				}
			}
		}
		return dhcp.ReplyPacket(p, dhcp.NAK, h.ip, nil, 0, nil)

	case dhcp.Release, dhcp.Decline:
		nic := p.CHAddr().String()
		for i, v := range h.leases {
			if v.nic == nic {
				delete(h.leases, i)
				break
			}
		}
	}
	return nil
}

func (h *DHCPHandler) freeLease() int {
	now := time.Now()
	b := rand.Intn(h.leaseRange) // Try random first
	for _, v := range [][]int{[]int{b, h.leaseRange}, []int{0, b}} {
		for i := v[0]; i < v[1]; i++ {
			if l, ok := h.leases[i]; !ok || l.expiry.Before(now) {
				return i
			}
		}
	}
	return -1
}


func SendDiscovery() {
	log.Println("SendDiscovery")
	time.Sleep(2 * time.Second)
	log.Println("SendDiscovery after sleeping")
	var err error

	//Create a connection to use
	//We need to set the connection ports to 1068 and 1067 so we don't need root access
	c, err := dhcp4client.NewInetSock(
		// 0.0.0.0: can not send: network is unreachable
		//dhcp4client.SetLocalAddr(net.UDPAddr{IP: net.IPv4(0, 0, 0, 0), Port: 68}),

		// for test on 192.168.1.2
		//dhcp4client.SetLocalAddr(net.UDPAddr{IP: net.IPv4(192, 168, 1, 2), Port: 68}),

		dhcp4client.SetLocalAddr(net.UDPAddr{IP: net.IPv4(192, 168, 1, 3), Port: 68}),
		//dhcp4client.SetLocalAddr(net.UDPAddr{IP: net.IPv4(0, 0, 0, 0), Port: 1068}),
		dhcp4client.SetRemoteAddr(net.UDPAddr{IP: net.IPv4bcast, Port: 67}))
	if err != nil {
		log.Println("Client Connection Generation:" + err.Error())
	}
	defer c.Close()

	m, err := net.ParseMAC("08-00-27-00-A8-E8")
	if err != nil {
		log.Printf("MAC Error:%v\n", err)
	}
	exampleClient, err := dhcp4client.New(dhcp4client.HardwareAddr(m), dhcp4client.Connection(c))
	if err != nil {
		log.Printf("Error:%v\n", err)
		return
	}
	defer exampleClient.Close()

	//success, acknowledgementpacket, err := exampleClient.Request()
	success, acknowledgementpacket, err := exampleClient.DiscoverAndOffer()

	log.Println("Success:", success)
	log.Println("Packet:", acknowledgementpacket)
}