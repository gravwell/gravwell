/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package plugins

import (
	"errors"
	"fmt"
	"net/rpc"
	"os"
	"os/exec"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	hclog "github.com/hashicorp/go-hclog"
	plugin "github.com/hashicorp/go-plugin"
)

const (
	ProtocolVersion        = 1
	MagicKey               = `GRAVWELL_INGESTER_PLUGIN`
	MagicValue             = `GravwellIngesterProcessorPluginsAreCool`
	PluginName             = `GravwellIngesterProcessorPlugin`
	pathVar         string = `Plugin_Path`

	startTimeout time.Duration = 500 * time.Millisecond
)

var (
	ErrMissingPluginPath  = errors.New("Plugin-Path config variable is missing")
	ErrNotReady           = errors.New("Plugin not ready")
	ErrPluginNotLoaded    = errors.New("Plugin executable not loaded")
	ErrPluginNotConnected = errors.New("Plugin connection not active")
	ErrInvalidParameters  = errors.New("RPC call parameters are invalid")
)

type IngesterProcessorPlugin struct {
	impl Plugin
	cli  *plugin.Client
	cp   plugin.ClientProtocol
}

type Plugin interface {
	// Plugins implement a subset of the processor values
	Process([]*entry.Entry) ([]*entry.Entry, error) //process an data item potentially setting a tag
	Flush() []*entry.Entry
	LoadConfig(map[string][]string) error //plus config interface
}

type PluginConfig struct {
	Plugin_Path string
	Verbose     bool
	Variables   map[string][]string
}

type PluginResponse struct {
	Ents  []*entry.Entry
	Error error
}

// HandshakeConfig to negotiate the plugin child, this is NOT a security feature
// Export so plugins can just straight use this
var HandshakeConfig = plugin.HandshakeConfig{
	ProtocolVersion:  ProtocolVersion,
	MagicCookieKey:   MagicKey,
	MagicCookieValue: MagicValue,
}

func PluginLoadConfig(vc *config.VariableConfig) (pc PluginConfig, err error) {
	var fi os.FileInfo
	//grab the plugin path variable
	if err = vc.MapTo(&pc); err != nil {
		return
	} else if pc.Plugin_Path == `` {
		fmt.Printf("%+v\n", vc)
		err = ErrMissingPluginPath
		return
	}
	//make sure its a file and that we have read and execute permissions
	if fi, err = os.Stat(pc.Plugin_Path); err != nil {
		return
	} else if !fi.Mode().IsRegular() {
		err = fmt.Errorf("%s is not a file", pc.Plugin_Path)
		return
	} else if pc.Variables, err = vc.VariableMap(); err != nil {
		return
	}

	err = testPlugin(pc)
	return
}

func newClient(pc PluginConfig) (c *plugin.Client, cp plugin.ClientProtocol, p Plugin, err error) {
	var ok bool
	var raw interface{}
	pluginMap := map[string]plugin.Plugin{
		PluginName: &IngesterProcessorPlugin{
			impl: p,
			cli:  c,
			cp:   cp,
		},
	}
	var lgr hclog.Logger
	if pc.Verbose {
		lgr = hclog.Default()
		lgr.SetLevel(hclog.Debug)
	} else {
		lgr = hclog.NewNullLogger()
	}
	// We're a host! Start by launching the plugin process.
	c = plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig: HandshakeConfig,
		Plugins:         pluginMap,
		Cmd:             exec.Command(pc.Plugin_Path),
		Logger:          lgr,
		StartTimeout:    startTimeout,
		Managed:         true,
	})

	// Connect via RPC
	if cp, err = c.Client(); err != nil {
		c.Kill()
		return
	}

	// Request the plugin
	if raw, err = cp.Dispense(PluginName); err != nil {
		c.Kill()
		return
	}

	// We should have an IngesterProcessorPlugin now, cast it to the appropriate
	// interface and make sure everything works
	if p, ok = raw.(*procPluginClient); !ok {
		c.Kill()
		c = nil
		p = nil
		err = fmt.Errorf("Plugin %s does not implement the Plugin interface", pc.Plugin_Path)
		fmt.Printf("%T %+v\n", raw, raw)
	}
	return

}

func NewIngesterProcessorPlugin(pc PluginConfig) (*IngesterProcessorPlugin, error) {
	c, cp, p, err := newClient(pc)
	if err != nil {
		return nil, err
	}
	return &IngesterProcessorPlugin{
		impl: p,
		cli:  c,
		cp:   cp,
	}, nil
}

// testPlugin will walk the process of getting a plugin up and rolling and call its loadConfig function
// this is expensive but should occur very rarely, so it is ok to take the hit
func testPlugin(pc PluginConfig) error {
	client, _, pg, err := newClient(pc)
	if err != nil {
		return err
	}
	defer client.Kill()

	//perform the config load
	if err = pg.LoadConfig(pc.Variables); err != nil {
		return err
	}
	return nil
}

func (p *IngesterProcessorPlugin) Client(b *plugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return &procPluginClient{
		c: c,
	}, nil
}

// Server returns the server implementation, this is the system that receives RPC calls and actually
// implements the plugin interface.  Basically the actual plugin part.
func (p *IngesterProcessorPlugin) Server(*plugin.MuxBroker) (interface{}, error) {
	if p.impl == nil {
		fmt.Printf("Server %s\n%+v\n", os.Args[0], p)
		return nil, ErrNotReady
	}
	return &procPluginServer{
		impl: p.impl,
	}, nil
}

func (pc PluginConfig) Get(name string) (val string, ok bool) {
	var vals []string
	if vals, ok = pc.Variables[name]; ok {
		if len(vals) > 0 {
			val = vals[0]
		} else {
			ok = false //kill it
		}
	}
	return
}

func (ipp *IngesterProcessorPlugin) check() (err error) {
	if ipp == nil || ipp.impl == nil {
		err = ErrNotReady
	} else if ipp.cli == nil {
		err = ErrPluginNotLoaded
	} else if ipp.cp == nil {
		err = ErrPluginNotConnected
	}
	return
}

func (ipp *IngesterProcessorPlugin) Process(ents []*entry.Entry) (ret []*entry.Entry, err error) {
	if err = ipp.check(); err == nil {
		ents, err = ipp.impl.Process(ents)
	}
	return
}

func (ipp *IngesterProcessorPlugin) LoadConfig(vars map[string][]string) (err error) {
	if err = ipp.check(); err == nil {
		err = ipp.impl.LoadConfig(vars)
	}
	return
}

func (ipp *IngesterProcessorPlugin) Flush() (ents []*entry.Entry) {
	if err := ipp.check(); err == nil {
		ents = ipp.impl.Flush()
	}
	return
}

func (ipp *IngesterProcessorPlugin) Close() (err error) {
	if err = ipp.check(); err != nil {
		return
	}
	ipp.cli.Kill()
	ipp.cli = nil
	ipp.cp = nil
	ipp.impl = nil
	return
}

type procPluginClient struct {
	c *rpc.Client
}

func (ppc *procPluginClient) Process(ents []*entry.Entry) (ret []*entry.Entry, err error) {
	var resp PluginResponse
	if ppc == nil || ppc.c == nil {
		err = ErrNotReady
	} else if err = ppc.c.Call("Plugin.Process", ents, &resp); err == nil {
		ret, err = resp.Ents, resp.Error
	}
	return
}

func (ppc *procPluginClient) Flush() (ents []*entry.Entry) {
	if ppc == nil || ppc.c == nil {
		return
	}
	var resp PluginResponse
	if err := ppc.c.Call("Plugin.Flush", nil, &resp); err == nil && resp.Error == nil {
		ents = resp.Ents
	}
	return
}

func (ppc *procPluginClient) LoadConfig(vars map[string][]string) (err error) {
	var resp PluginResponse
	if ppc == nil || ppc.c == nil {
		err = ErrNotReady
	} else {
		if err = ppc.c.Call("Plugin.LoadConfig", vars, &resp); err == nil {
			err = resp.Error
		}
	}
	return
}

func ServePlugin(impl Plugin) {
	plugin.Serve(&plugin.ServeConfig{
		Logger:          hclog.Default(),
		HandshakeConfig: HandshakeConfig,
		Plugins: map[string]plugin.Plugin{
			PluginName: &IngesterProcessorPlugin{impl: impl},
		},
	})
}

type procPluginServer struct {
	impl Plugin
}

func (pps *procPluginServer) Flush(_ interface{}, resp *PluginResponse) (err error) {
	if resp == nil {
		return ErrInvalidParameters //what else do we do?
	} else if pps.impl == nil {
		resp.Error = ErrNotReady
		err = ErrNotReady
	} else {
		resp.Ents = pps.impl.Flush()
	}
	return
}

func (pps *procPluginServer) Process(ents []*entry.Entry, resp *PluginResponse) (err error) {
	if resp == nil {
		err = ErrInvalidParameters
	} else if pps.impl == nil {
		resp.Error = ErrNotReady
		err = ErrNotReady
	} else {
		resp.Ents, resp.Error = pps.impl.Process(ents)
	}
	return
}

func (pps *procPluginServer) LoadConfig(vars map[string][]string, resp *PluginResponse) (err error) {
	if resp == nil {
		err = ErrInvalidParameters
	} else if pps.impl == nil {
		resp.Error = ErrNotReady
		err = ErrNotReady
	} else {
		resp.Error = pps.impl.LoadConfig(vars)
	}
	return
}
