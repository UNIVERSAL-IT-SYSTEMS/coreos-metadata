// Copyright 2015 CoreOS, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/user"
	"path"
	"strings"

	"github.com/coreos/coreos-metadata/internal/providers"
	"github.com/coreos/coreos-metadata/internal/providers/azure"
	"github.com/coreos/coreos-metadata/internal/providers/ec2"
	"github.com/coreos/coreos-metadata/internal/providers/gce"
	"github.com/coreos/coreos-metadata/internal/providers/packet"

	"github.com/coreos/update-ssh-keys/authorized_keys_d"
)

var (
	version       = "was not built properly"
	versionString = fmt.Sprintf("coreos-metadata %s", version)
)

const (
	cmdlinePath    = "/proc/cmdline"
	cmdlineOEMFlag = "coreos.oem.id"
)

func main() {
	flags := struct {
		cmdline    bool
		provider   string
		attributes string
		sshKeys    string
		version    bool
	}{}

	flag.BoolVar(&flags.cmdline, "cmdline", false, "Read the cloud provider from the kernel cmdline")
	flag.StringVar(&flags.provider, "provider", "", "The name of the cloud provider")
	flag.StringVar(&flags.attributes, "attributes", "", "The file into which the metadata attributes are written")
	flag.StringVar(&flags.sshKeys, "ssh-keys", "", "Update SSH keys for the given user")
	flag.BoolVar(&flags.version, "version", false, "Print the version and exit")

	flag.Parse()

	if flags.version {
		fmt.Println(versionString)
		return
	}

	if flags.cmdline && flags.provider == "" {
		args, err := ioutil.ReadFile(cmdlinePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "could not read cmdline: %v\n", err)
			os.Exit(2)
		}

		flags.provider = parseCmdline(args)
	}

	switch flags.provider {
	case "azure", "ec2", "gce", "packet":
	default:
		fmt.Fprintf(os.Stderr, "invalid provider %q\n", flags.provider)
		os.Exit(2)
	}

	metadata, err := fetchMetadata(flags.provider)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to fetch metadata: %v\n", err)
		os.Exit(1)
	}

	if err := writeMetadataAttributes(flags.attributes, metadata); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write metadata attributes: %v\n", err)
		os.Exit(1)
	}

	if err := writeMetadataKeys(flags.sshKeys, metadata); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write metadata keys: %v\n", err)
		os.Exit(1)
	}
}

func parseCmdline(cmdline []byte) (oem string) {
	for _, arg := range strings.Split(string(cmdline), " ") {
		parts := strings.SplitN(strings.TrimSpace(arg), "=", 2)
		key := parts[0]

		if key != cmdlineOEMFlag {
			continue
		}

		if len(parts) == 2 {
			oem = parts[1]
		}
	}

	return
}

func fetchMetadata(provider string) (providers.Metadata, error) {
	switch provider {
	case "azure":
		return azure.FetchMetadata()
	case "ec2":
		return ec2.FetchMetadata()
	case "gce":
		return gce.FetchMetadata()
	case "packet":
		return packet.FetchMetadata()
	default:
		panic("bad provider")
	}
}

func writeVariable(out *os.File, key string, value string) (err error) {
	if len(value) > 0 {
		_, err = fmt.Fprintf(out, "COREOS_%s=%s\n", key, value)
	}
	return
}

func writeMetadataAttributes(attributes string, metadata providers.Metadata) error {
	if attributes == "" {
		return nil
	}

	if err := os.MkdirAll(path.Dir(attributes), 0755); err != nil {
		fmt.Fprintf(os.Stderr, "failed to create directory: %v\n", err)
		os.Exit(1)
	}

	out, err := os.Create(attributes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create file: %v\n", err)
		os.Exit(1)
	}
	defer out.Close()

	for key, value := range metadata.Attributes {
		if err := writeVariable(out, key, value); err != nil {
			return err
		}
	}
	return nil
}

func writeMetadataKeys(username string, metadata providers.Metadata) error {
	if username == "" || metadata.SshKeys == nil {
		return nil
	}

	usr, err := user.Lookup(username)
	if err != nil {
		return fmt.Errorf("unable to lookup user %q: %v", username, err)
	}

	akd, err := authorized_keys_d.Open(usr, true)
	if err != nil {
		return err
	}
	defer akd.Close()

	ks := strings.Join(metadata.SshKeys, "\n")
	if err := akd.Add("coreos-metadata", []byte(ks), true, true); err != nil {
		return err
	}

	return akd.Sync()
}
