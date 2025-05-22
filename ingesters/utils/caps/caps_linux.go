//go:build linux
// +build linux

/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package caps implements some helpers to check capabilities on a file on Linux
package caps

import (
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	linuxCapV3 = 0x20080522

	All Capabilities = 0xffffffffffffffff
)

// stolen directly from: https://pkg.go.dev/kernel.org/pub/linux/libs/security/libcap/cap#Value
// we are choosing to treat this a BSD 3-clause as laid out in the license note:
// https://pkg.go.dev/kernel.org/pub/linux/libs/security/libcap/cap#section-readme
const (
	// CHOWN allows a process to arbitrarily change the user and
	// group ownership of a file.
	CHOWN Capabilities = iota

	// DAC_OVERRIDE allows a process to override of all Discretionary
	// Access Control (DAC) access, including ACL execute
	// access. That is read, write or execute files that the
	// process would otherwise not have access to. This
	// excludes DAC access covered by cap.LINUX_IMMUTABLE.
	DAC_OVERRIDE

	// DAC_READ_SEARCH allows a process to override all DAC restrictions
	// limiting the read and search of files and
	// directories. This excludes DAC access covered by
	// cap.LINUX_IMMUTABLE.
	DAC_READ_SEARCH

	// FOWNER allows a process to perform operations on files, even
	// where file owner ID should otherwise need be equal to
	// the UID, except where cap.FSETID is applicable. It
	// doesn't override MAC and DAC restrictions.
	FOWNER

	// FSETID allows a process to set the S_ISUID and S_ISUID bits of
	// the file permissions, even when the process' effective
	// UID or GID/supplementary GIDs do not match that of the
	// file.
	FSETID

	// KILL allows a process to send a kill(2) signal to any other
	// process - overriding the limitation that there be a
	// [E]UID match between source and target process.
	KILL

	// SETGID allows a process to freely manipulate its own GIDs:
	//   - arbitrarily set the GID, EGID, REGID, RESGID values
	//   - arbitrarily set the supplementary GIDs
	//   - allows the forging of GID credentials passed over a
	//     socket
	SETGID

	// SETUID allows a process to freely manipulate its own UIDs:
	//   - arbitrarily set the UID, EUID, REUID and RESUID
	//     values
	//   - allows the forging of UID credentials passed over a
	//     socket
	SETUID

	// SETPCAP allows a process to freely manipulate its inheritable
	// capabilities.
	//
	// Linux supports the POSIX.1e Inheritable set, the POXIX.1e (X
	// vector) known in Linux as the Bounding vector, as well as
	// the Linux extension Ambient vector.
	//
	// This capability permits dropping bits from the Bounding
	// vector (ie. raising B bits in the libcap IAB
	// representation). It also permits the process to raise
	// Ambient vector bits that are both raised in the Permitted
	// and Inheritable sets of the process. This capability cannot
	// be used to raise Permitted bits, Effective bits beyond those
	// already present in the process' permitted set, or
	// Inheritable bits beyond those present in the Bounding
	// vector.
	//
	// [Historical note: prior to the advent of file capabilities
	// (2008), this capability was suppressed by default, as its
	// unsuppressed behavior was not auditable: it could
	// asynchronously grant its own Permitted capabilities to and
	// remove capabilities from other processes arbitrarily. The
	// former leads to undefined behavior, and the latter is better
	// served by the kill system call.]
	SETPCAP

	// LINUX_IMMUTABLE allows a process to modify the S_IMMUTABLE and
	// S_APPEND file attributes.
	LINUX_IMMUTABLE

	// NET_BIND_SERVICE allows a process to bind to privileged ports:
	//   - TCP/UDP sockets below 1024
	//   - ATM VCIs below 32
	NET_BIND_SERVICE

	// NET_BROADCAST allows a process to broadcast to the network and to
	// listen to multicast.
	NET_BROADCAST

	// NET_ADMIN allows a process to perform network configuration
	// operations:
	//   - interface configuration
	//   - administration of IP firewall, masquerading and
	//     accounting
	//   - setting debug options on sockets
	//   - modification of routing tables
	//   - setting arbitrary process, and process group
	//     ownership on sockets
	//   - binding to any address for transparent proxying
	//     (this is also allowed via cap.NET_RAW)
	//   - setting TOS (Type of service)
	//   - setting promiscuous mode
	//   - clearing driver statistics
	//   - multicasing
	//   - read/write of device-specific registers
	//   - activation of ATM control sockets
	NET_ADMIN

	// NET_RAW allows a process to use raw networking:
	//   - RAW sockets
	//   - PACKET sockets
	//   - binding to any address for transparent proxying
	//     (also permitted via cap.NET_ADMIN)
	NET_RAW

	// IPC_LOCK allows a process to lock shared memory segments for IPC
	// purposes.  Also enables mlock and mlockall system
	// calls.
	IPC_LOCK

	// IPC_OWNER allows a process to override IPC ownership checks.
	IPC_OWNER

	// SYS_MODULE allows a process to initiate the loading and unloading
	// of kernel modules. This capability can effectively
	// modify kernel without limit.
	SYS_MODULE

	// SYS_RAWIO allows a process to perform raw IO:
	//   - permit ioper/iopl access
	//   - permit sending USB messages to any device via
	//     /dev/bus/usb
	SYS_RAWIO

	// SYS_CHROOT allows a process to perform a chroot syscall to change
	// the effective root of the process' file system:
	// redirect to directory "/" to some other location.
	SYS_CHROOT

	// SYS_PTRACE allows a process to perform a ptrace() of any other
	// process.
	SYS_PTRACE

	// SYS_PACCT allows a process to configure process accounting.
	SYS_PACCT

	// SYS_ADMIN allows a process to perform a somewhat arbitrary
	// grab-bag of privileged operations. Over time, this
	// capability should weaken as specific capabilities are
	// created for subsets of cap.SYS_ADMINs functionality:
	//   - configuration of the secure attention key
	//   - administration of the random device
	//   - examination and configuration of disk quotas
	//   - setting the domainname
	//   - setting the hostname
	//   - calling bdflush()
	//   - mount() and umount(), setting up new SMB connection
	//   - some autofs root ioctls
	//   - nfsservctl
	//   - VM86_REQUEST_IRQ
	//   - to read/write pci config on alpha
	//   - irix_prctl on mips (setstacksize)
	//   - flushing all cache on m68k (sys_cacheflush)
	//   - removing semaphores
	//   - Used instead of cap.CHOWN to "chown" IPC message
	//     queues, semaphores and shared memory
	//   - locking/unlocking of shared memory segment
	//   - turning swap on/off
	//   - forged pids on socket credentials passing
	//   - setting readahead and flushing buffers on block
	//     devices
	//   - setting geometry in floppy driver
	//   - turning DMA on/off in xd driver
	//   - administration of md devices (mostly the above, but
	//     some extra ioctls)
	//   - tuning the ide driver
	//   - access to the nvram device
	//   - administration of apm_bios, serial and bttv (TV)
	//     device
	//   - manufacturer commands in isdn CAPI support driver
	//   - reading non-standardized portions of PCI
	//     configuration space
	//   - DDI debug ioctl on sbpcd driver
	//   - setting up serial ports
	//   - sending raw qic-117 commands
	//   - enabling/disabling tagged queuing on SCSI
	//     controllers and sending arbitrary SCSI commands
	//   - setting encryption key on loopback filesystem
	//   - setting zone reclaim policy
	SYS_ADMIN

	// SYS_BOOT allows a process to initiate a reboot of the system.
	SYS_BOOT

	// SYS_NICE allows a process to maipulate the execution priorities
	// of arbitrary processes:
	//   - those involving different UIDs
	//   - setting their CPU affinity
	//   - alter the FIFO vs. round-robin (realtime)
	//     scheduling for itself and other processes.
	SYS_NICE

	// SYS_RESOURCE allows a process to adjust resource related parameters
	// of processes and the system:
	//   - set and override resource limits
	//   - override quota limits
	//   - override the reserved space on ext2 filesystem
	//     (this can also be achieved via cap.FSETID)
	//   - modify the data journaling mode on ext3 filesystem,
	//     which uses journaling resources
	//   - override size restrictions on IPC message queues
	//   - configure more than 64Hz interrupts from the
	//     real-time clock
	//   - override the maximum number of consoles for console
	//     allocation
	//   - override the maximum number of keymaps
	SYS_RESOURCE

	// SYS_TIME allows a process to perform time manipulation of clocks:
	//   - alter the system clock
	//   - enable irix_stime on MIPS
	//   - set the real-time clock
	SYS_TIME

	// SYS_TTY_CONFIG allows a process to manipulate tty devices:
	//   - configure tty devices
	//   - perform vhangup() of a tty
	SYS_TTY_CONFIG

	// MKNOD allows a process to perform privileged operations with
	// the mknod() system call.
	MKNOD

	// LEASE allows a process to take leases on files.
	LEASE

	// AUDIT_WRITE allows a process to write to the audit log via a
	// unicast netlink socket.
	AUDIT_WRITE

	// AUDIT_CONTROL allows a process to configure audit logging via a
	// unicast netlink socket.
	AUDIT_CONTROL

	// SETFCAP allows a process to set capabilities on files.
	// Permits a process to uid_map the uid=0 of the
	// parent user namespace into that of the child
	// namespace. Also, permits a process to override
	// securebits locks through user namespace
	// creation.
	SETFCAP

	// MAC_OVERRIDE allows a process to override Manditory Access Control
	// (MAC) access. Not all kernels are configured with a MAC
	// mechanism, but this is the capability reserved for
	// overriding them.
	MAC_OVERRIDE

	// MAC_ADMIN allows a process to configure the Mandatory Access
	// Control (MAC) policy. Not all kernels are configured
	// with a MAC enabled, but if they are this capability is
	// reserved for code to perform administration tasks.
	MAC_ADMIN

	// SYSLOG allows a process to configure the kernel's syslog
	// (printk) behavior.
	SYSLOG

	// WAKE_ALARM allows a process to trigger something that can wake the
	// system up.
	WAKE_ALARM

	// BLOCK_SUSPEND allows a process to block system suspends - prevent the
	// system from entering a lower power state.
	BLOCK_SUSPEND

	// AUDIT_READ allows a process to read the audit log via a multicast
	// netlink socket.
	AUDIT_READ

	// PERFMON allows a process to enable observability of privileged
	// operations related to performance. The mechanisms
	// include perf_events, i915_perf and other kernel
	// subsystems.
	PERFMON

	// BPF allows a process to manipulate aspects of the kernel
	// enhanced Berkeley Packet Filter (BPF) system. This is
	// an execution subsystem of the kernel, that manages BPF
	// programs. cap.BPF permits a process to:
	//   - create all types of BPF maps
	//   - advanced verifier features:
	//     - indirect variable access
	//     - bounded loops
	//     - BPF to BPF function calls
	//     - scalar precision tracking
	//     - larger complexity limits
	//     - dead code elimination
	//     - potentially other features
	//
	// Other capabilities can be used together with cap.BFP to
	// further manipulate the BPF system:
	//   - cap.PERFMON relaxes the verifier checks as follows:
	//     - BPF programs can use pointer-to-integer
	//       conversions
	//     - speculation attack hardening measures can be
	//       bypassed
	//     - bpf_probe_read to read arbitrary kernel memory is
	//       permitted
	//     - bpf_trace_printk to print the content of kernel
	//       memory
	//   - cap.SYS_ADMIN permits the following:
	//     - use of bpf_probe_write_user
	//     - iteration over the system-wide loaded programs,
	//       maps, links BTFs and convert their IDs to file
	//       descriptors.
	//   - cap.PERFMON is required to load tracing programs.
	//   - cap.NET_ADMIN is required to load networking
	//     programs.
	BPF

	// CHECKPOINT_RESTORE allows a process to perform checkpoint
	// and restore operations. Also permits
	// explicit PID control via clone3() and
	// also writing to ns_last_pid.
	CHECKPOINT_RESTORE
)

const (
	minCap = CHOWN
	maxCap = CHECKPOINT_RESTORE
)

type capHeader struct {
	version uint32
	pid     int
}

type capData struct {
	effective   uint32
	permitted   uint32
	inheritable uint32
}

type Capabilities uint64

func GetCaps() (c Capabilities, err error) {
	//check if we are running as root, if so, just return ALL caps
	if os.Getuid() == 0 || os.Geteuid() == 0 {
		c = All
		return
	}
	hdr := capHeader{
		version: linuxCapV3,
	}
	var data [2]capData
	_, _, e1 := unix.RawSyscall(unix.SYS_CAPGET, uintptr(unsafe.Pointer(&hdr)), uintptr(unsafe.Pointer(&data)), 0)
	if e1 != 0 {
		err = e1
		return
	}
	c = Capabilities(uint64(data[0].effective) | (uint64(data[1].effective) << 32))
	return
}

func (c Capabilities) Has(v Capabilities) bool {
	return (c & (1 << v)) != 0
}

func Has(v Capabilities) bool {
	if c, err := GetCaps(); err == nil {
		return c.Has(v)
	}
	return false
}
