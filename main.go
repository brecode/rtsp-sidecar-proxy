package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"gopkg.in/alecthomas/kingpin.v2"
	"gopkg.in/yaml.v2"
)

var Version = "v0.0.0"

type trackFlow int

const (
	_TRACK_FLOW_RTP trackFlow = iota
	_TRACK_FLOW_RTCP
)

type track struct {
	rtpPort  int
	rtcpPort int
}

type streamProtocol int

const (
	_STREAM_PROTOCOL_UDP streamProtocol = iota
	_STREAM_PROTOCOL_TCP
)

func (s streamProtocol) String() string {
	if s == _STREAM_PROTOCOL_UDP {
		return "udp"
	}
	return "tcp"
}

type streamConf struct {
	Url      string `yaml:"url"`
	Protocol string `yaml:"protocol"`
}

type conf struct {
	ReadTimeout  string `yaml:"readTimeout"`
	WriteTimeout string `yaml:"writeTimeout"`
	Server       struct {
		Protocols []string `yaml:"protocols"`
		RtspPort  int      `yaml:"rtspPort"`
		RtpPort   int      `yaml:"rtpPort"`
		RtcpPort  int      `yaml:"rtcpPort"`
		ReadUser  string   `yaml:"readUser"`
		ReadPass  string   `yaml:"readPass"`
	} `yaml:"server"`
}

func loadConf(confPath string) (*conf, error) {
	if confPath == "stdin" {
		var ret conf
		err := yaml.NewDecoder(os.Stdin).Decode(&ret)
		if err != nil {
			return nil, err
		}

		return &ret, nil

	} else {
		f, err := os.Open(confPath)
		if err != nil {
			return nil, err
		}
		defer f.Close()

		var ret conf
		err = yaml.NewDecoder(f).Decode(&ret)
		if err != nil {
			return nil, err
		}

		return &ret, nil
	}
}

type args struct {
	version  bool
	confPath string
}

type program struct {
	conf         conf
	readTimeout  time.Duration
	writeTimeout time.Duration
	protocols    map[streamProtocol]struct{}
	streams      map[string]*stream
	tcpl         *serverTcpListener
	udplRtp      *serverUdpListener
	udplRtcp     *serverUdpListener
}

func newProgram(sargs []string) (*program, error) {
	kingpin.CommandLine.Help = "rtsp-sidecar-proxy " + Version + "\n\n" +
		"RTSP proxy."

	argVersion := kingpin.Flag("version", "print version").Bool()
	argConfPath := kingpin.Arg("confpath", "path of a config file. The default is conf.yml. Use 'stdin' to read config from stdin").Default("/tmp/conf.yml").String()

	kingpin.MustParse(kingpin.CommandLine.Parse(sargs))

	args := args{
		version:  *argVersion,
		confPath: *argConfPath,
	}

	if args.version == true {
		fmt.Println(Version)
		os.Exit(0)
	}

	conf, err := loadConf(args.confPath)
	if err != nil {
		return nil, err
	}
	if err != nil {
		return nil, err
	}

	if conf.ReadTimeout == "" {
		conf.ReadTimeout = "5s"
	}
	if conf.WriteTimeout == "" {
		conf.WriteTimeout = "5s"
	}
	if len(conf.Server.Protocols) == 0 {
		conf.Server.Protocols = []string{"tcp", "udp"}
	}
	if conf.Server.RtspPort == 0 {
		conf.Server.RtspPort = 8554
	}
	if conf.Server.RtpPort == 0 {
		conf.Server.RtpPort = 8050
	}
	if conf.Server.RtcpPort == 0 {
		conf.Server.RtcpPort = 8051
	}

	readTimeout, err := time.ParseDuration(conf.ReadTimeout)
	if err != nil {
		return nil, fmt.Errorf("unable to parse read timeout: %s", err)
	}
	writeTimeout, err := time.ParseDuration(conf.WriteTimeout)
	if err != nil {
		return nil, fmt.Errorf("unable to parse write timeout: %s", err)
	}
	protocols := make(map[streamProtocol]struct{})
	for _, proto := range conf.Server.Protocols {
		switch proto {
		case "udp":
			protocols[_STREAM_PROTOCOL_UDP] = struct{}{}

		case "tcp":
			protocols[_STREAM_PROTOCOL_TCP] = struct{}{}

		default:
			return nil, fmt.Errorf("unsupported protocol: '%v'", proto)
		}
	}
	if len(protocols) == 0 {
		return nil, fmt.Errorf("no protocols provided")
	}
	if (conf.Server.RtpPort % 2) != 0 {
		return nil, fmt.Errorf("rtp port must be even")
	}
	if conf.Server.RtcpPort != (conf.Server.RtpPort + 1) {
		return nil, fmt.Errorf("rtcp port must be rtp port plus 1")
	}

	log.Printf("rtsp-sidecar-proxy %s", Version)

	p := &program{
		conf:         *conf,
		readTimeout:  readTimeout,
		writeTimeout: writeTimeout,
		protocols:    protocols,
		streams:      make(map[string]*stream),
	}

	p.udplRtp, err = newServerUdpListener(p, p.conf.Server.RtpPort, _TRACK_FLOW_RTP)
	if err != nil {
		return nil, err
	}

	p.udplRtcp, err = newServerUdpListener(p, p.conf.Server.RtcpPort, _TRACK_FLOW_RTCP)
	if err != nil {
		return nil, err
	}

	p.tcpl, err = newServerTcpListener(p)
	if err != nil {
		return nil, err
	}

	go p.udplRtp.run()
	go p.udplRtcp.run()
	go p.tcpl.run()

	log.Println("all listeners up.")

	return p, nil
}

func (p *program) close() {

	p.tcpl.close()
	p.udplRtcp.close()
	p.udplRtp.close()
}

func main() {
	_, err := newProgram(os.Args[1:])
	if err != nil {
		log.Fatal("ERR: ", err)
	}

	select {}
}
