package main

//---set GOARCH=386
import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	SERVICE_NAME = "pm_service"
)

func service_entry(binDir string) {
	//binDir := path.Dir(os.Args[0])
	configFile := fmt.Sprintf("%s\\portmap.txt", binDir)
	f, err := os.Open(configFile)
	if err != nil {
		log.Printf("portmap.txt open failed, %v\n", err)
		return
	}
	defer f.Close()
	r := bufio.NewReader(f)
	showdata := false
	if len(os.Args) > 1 && os.Args[1] == "show" {
		showdata = true
	}

	for {
		line, e1 := r.ReadString('\n')
		if e1 != nil {
			break
		}
		if line == "" {
			break
		}
		var port int
		var target string
		n, _ := fmt.Sscanf(line, "%d %s", &port, &target)
		if n != 2 {
			continue
		}

		log.Printf("%d--%s\n", port, target)
		go Listen(port, target, showdata)
	}
	for {
		time.Sleep(5 * time.Second)
	}
}

/*
func main11() {
	lstn_port := flag.Int("listen", 6666, "listen port")
	target_host := flag.String("target", "", "target host, like 1.1.1.1:12345")
	flag.Parse()
	if *target_host == "" {
		flag.Usage()
		return
	}

	listener, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", *lstn_port))
	if err != nil {
		println("error listening:", err.Error())
		os.Exit(1)
	}

	for {
		conn, err := listener.Accept()
		if err != nil {
			println("Error accept:", err.Error())
			break
		}
		conn1, err1 := net.Dial("tcp", *target_host)
		if err1 != nil {
			println("Connect to target failed, continue")
			conn.Close()
			continue
		}
		go Trans(conn, conn1, "<<")
		go Trans(conn1, conn, ">>")
	}
}
*/

const (
	RECV_BUF_LEN = 1024
)

func Listen(lstn_port int, target string, showdata bool) {
	listener, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", lstn_port))
	if err != nil {
		log.Println("error listening:", err.Error())
		os.Exit(1)
	}
	tag_up, tag_down := "<<", ">>"
	if !showdata {
		tag_up, tag_down = "", ""
	}

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Println("Error accept:", err.Error())
			break
		}
		conn1, err1 := net.Dial("tcp", target)
		if err1 != nil {
			println("Connect to target failed, continue")
			conn.Close()
			continue
		}
		go Trans(conn, conn1, tag_up)
		go Trans(conn1, conn, tag_down)
	}
}

func Trans(conn1, conn2 net.Conn, tag string) {
	buf := make([]byte, RECV_BUF_LEN)
	defer conn1.Close()
	defer conn2.Close()
	for {
		n, err := conn1.Read(buf)
		if err != nil {
			println("Error reading:", err.Error())
			break
		}
		if tag != "" {
			fmt.Printf("%s [%s]\n", tag, string(buf[:n]))
		}

		//send to target
		_, err = conn2.Write(buf[0:n])
		if err != nil {
			println("Error send reply:", err.Error())
			break
		}
	}

}

func EchoFunc(conn net.Conn) {
	buf := make([]byte, RECV_BUF_LEN)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			println("Error reading:", err.Error())
			break
		}
		println("received ", n, " bytes of data =", string(buf))

		//send reply
		_, err = conn.Write(buf)
		if err != nil {
			println("Error send reply:", err.Error())
		} else {
			println("Reply sent")
		}
	}
	fmt.Printf("close one\n")
	conn.Close()
}

func client() {
	if len(os.Args) != 2 {
		fmt.Println("Usage: ", os.Args[0], "host")
		os.Exit(1)
	}
	host := os.Args[1]
	conn, err := net.Dial("tcp", host+":8080")
	if err != nil {
		println("Connect failed")
		return
	}
	_, err = conn.Write([]byte("HEAD"))
	reader := bufio.NewReader(os.Stdin)
	for {
		line, err := reader.ReadString('\n')
		fmt.Println(err)
		line = strings.TrimRight(line, " \t\r\n")
		if err != nil {
			conn.Close()
			break
		}
	}
}

//===============================================
type Port struct {
	remote_addr string // "x.x.x.x:nnn"
	listen_port int
	last_active time.Time
	running     bool
	listener    net.Listener
}

func (p *Port) Str() string {
	return fmt.Sprintf("%d => %s  %s", p.listen_port, p.remote_addr, p.last_active)
}

type Serv struct {
	ports    map[int]*Port
	showdata *bool
	mng_port *int
}

func NewServ() *Serv {
	v := &Serv{
		make(map[int]*Port),
		flag.Bool("showdata", false, "if show transfer data"),
		flag.Int("port", 8181, "manage port")}

	return v
}

func (v *Serv) Run() {
	flag.Parse()

	http.HandleFunc("/bind", v.Bind)
	http.HandleFunc("/unbind", v.Unbind)
	http.HandleFunc("/list", v.List)

	http.ListenAndServe(fmt.Sprintf(":%d", *(v.mng_port)), nil)
}

func (v *Serv) Bind(w http.ResponseWriter, r *http.Request) {
	u := r.URL
	local_port := u.Query().Get("local_port")
	var port int
	fmt.Sscanf(u.Query().Get("port"), "%d", &port)
	_addr := r.RemoteAddr
	_addr = strings.Split(_addr, ":")[0]
	remote_addr := fmt.Sprintf("%s:%s", _addr, local_port)

	target_addr := u.Query().Get("target_addr")
	if target_addr != "" {
		remote_addr = target_addr
	}

	p, prs := v.ports[port]
	if !prs {
		p = &Port{remote_addr: remote_addr,
			listen_port: port, running: true,
			last_active: time.Now(),
			listener:    nil}
		v.ports[port] = p
		fmt.Printf("bind %d to %s\n", port, remote_addr)
		go v.Listen(p, *(v.showdata))
		log.Printf("--new Listen: %s", p.Str())
	} else {
		p.remote_addr = remote_addr
		p.last_active = time.Now()
	}

	fmt.Fprintf(w, "ok\n")
}
func (v *Serv) List(w http.ResponseWriter, r *http.Request) {
	for _, p := range v.ports {
		//		fmt.Fprintf(w, "%d => %s  %s\n", p.listen_port, p.remote_addr, p.last_active)
		fmt.Fprintf(w, "%s\n", p.Str())
	}
}
func (v *Serv) Listen(port *Port, showdata bool) {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port.listen_port))
	if err != nil {
		log.Println("error listening:", err.Error())
		os.Exit(1)
	}
	tag_up, tag_down := "<<", ">>"
	if !showdata {
		tag_up, tag_down = "", ""
	}
	port.listener = listener

	for port.running {
		conn, err := listener.Accept()
		if err != nil {
			log.Println("Error accept:", err.Error())
			break
		}
		conn1, err1 := net.Dial("tcp", port.remote_addr)
		if err1 != nil {
			println("Connect to target failed, continue")
			conn.Close()
			continue
		}
		go Trans(conn, conn1, tag_up)
		go Trans(conn1, conn, tag_down)
	}
}

func (v *Serv) Unbind(w http.ResponseWriter, r *http.Request) {
	u := r.URL
	var port int
	fmt.Sscanf(u.Query().Get("port"), "%d", &port)
	p, prs := v.ports[port]
	if prs {
		p.running = false
		p.listener.Close()
		delete(v.ports, port)
		fmt.Fprintf(w, "ok\n")
	} else {
		fmt.Fprintf(w, "failed, [not found]\n")
	}
}

/*
curl "http://172.21.3.187:8181/bind?target_addr=172.22.0.226:7070&port=7071"
curl "http://172.21.3.187:8181/bind?target_addr=172.22.0.23:8989&port=8989"
curl "http://172.21.3.187:8181/list
*/

func main() {
	serv := NewServ()
	serv.Run()
}
