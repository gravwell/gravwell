# Kitctl

`kitctl` is a command-line tool for working with Gravwell kits. It is designed so users can more easily maintain kits in version control repos.

## Commands and Flags

Kitctl's basic syntax is similar to the `go` tool:

	kitctl [flags] <command> [arguments]

The commands are:

* `unpack`: unpack a kit file
* `pack`: pack the kit into a file
* `info`: print information about the kit
* `init`: start a new kit from scratch
* `configmacro`: manage config macros
* `dep`: manage dependencies

Commands may have sub-commands, which are presented as additional arguments. For example, to create a new config macro, use the "add" sub-command: `kitctl configmacro add`.

Flags are specified before the command, e.g. `kitctl -name "My Kit" init`. See each command's section for a list of flags it expects.

## Unpack a Kit

To unpack a kit into the current directory, run `kitctl unpack` and give it the path to a kit file:

	; kitctl unpack /tmp/ipmi.kit

This creates a file named `MANIFEST` and individual directories for each type of object in the kit:

	MANIFEST		file			macro			playbook		template
	dashboard		license			pivot			searchlibrary

## Pack a Kit

To pack a kit from the current directory, run `kitctl pack` and give it the desired output filename:

	; kitctl pack /tmp/mykit.kit

## Get Kit Info

The `kitctl info` command gives information about the kit in the current directory:

	; kitctl info
	•Kit ID: io.gravwell.ipmi
	•Name: IPMI
	•Description: An IPMI kit that pairs with the Gravwell IPMI ingester.
	•Version: 2
	•Minimum Gravwell version required: 4.1.6
	•Maximum Gravwell version allowed: 0.0.0
	•Dependencies:
		none
	•Items:
		license			BSD-2
		macro			IPMI
		dashboard			46495878862902
		dashboard			249638155122607
		dashboard			205736850289731
		template			22e76583-e349-4400-8fc4-b238378a9b23
		[...]
		file			070e37c1-051e-4eb5-9126-c346b970ad89
		playbook			c6032ccd-790e-4361-ab10-1620b6d98272

## Create a New Kit

Use the `init` command to start a new kit from scratch. Be aware that building a kit this way is challenging; we generally recommend building the initial kit within Gravwell, then migrating it to a version-controlled repository using `kitctl unpack`.

	; kitctl -name 'Sample Kit' -desc 'A sample kit' -id 'io.gravwell.sample' -minver '3.2.0' init

The `init` command accepts the following flags:

* `-id <kit id>` specifies the unique ID for the kit, e.g. `-id io.gravwell.networkenrichment`
* `-name <kit name>` specifies the human-friendly name of the kit
* `-desc <description>` sets a more detailed description for the kit
* `-version <version>` (optional) sets the version for the kit. Defaults to 1.
* `-minver <version>` (optional) sets a minimum Gravwell version for the kit, e.g. `-minver 4.1.2`
* `-maxver <version>` (optional) sets a maximum Gravwell version for the kit, e.g. `-maxver 5.0.0`.

NOTE: Most users should not use the `-maxver` flag without very good reason. Gravwell makes great efforts to support backward compatibility, so kits will typically work fine in newer versions of Gravwell.

## Manage Config Macros

Config Macros are a special kind of macro. During kit installation, Gravwell will prompt the user to set values for the config macros. The `configmacro` command offers several subcommands to manage config macros.

Config macros have two types: "tag" and "other". The type helps the Gravwell GUI prompt the user for potential values. If set to "tag", the GUI will only allow the user to specify tags. If set to "other", the user may enter any value.

### configmacro list

This subcommand lists the names of all config macros in the current kit.

	; kitctl configmacro list
	FOO
	BAR

### configmacro show

This subcommand prints information about a specific config macro, selected using the `-name` flag:

	; kitctl -name FOO configmacro show
	Name: FOO
	Description: My config macro
	Default value: changeme
	Type: OTHER

### configmacro add

This subcommand adds a new config macro to the current kit. It requires the following flags:

* `-name <name>` sets the macro name. Note that macros must be upper-case alphanumeric strings; the only other characters allowed are dash (-) and underscore (_).
* `-desc <description>` sets a human-friendly description for the macro.
* `-default-value <value>` specifies a default value for the macro.
* `-macro-type <type>` sets the type of the macro. This should be either "tag" or "other".

To add a macro named `IPMI_TAG` which defines the tag containing IPMI entries, with a default value of `ipmi`, run this:

	; kitctl -name 'IPMI_TAG' -desc 'Tag containing IPMI entries' -default-value ipmi -macro-type tag configmacro add

Note that kitctl will check the macro name against existing config macros *and* against all *regular* macros. It will reject any name that conflicts with an existing one.

### configmacro del

This subcommand deletes an existing config macro. The name of the macro to delete is specified by the `-name` flag:

	; kitctl -name IPMI_TAG configmacro del

## Manage Dependencies

Kits may specify dependencies, additional kits which must be installed before that kit can itself be installed. A dependency consists of a kit ID (e.g. `io.gravwell.networkenrichment`) and a minimum kit version. Kitctl manages dependencies using the `dep` command.

### dep list

This subcommand lists all dependencies defined for the current kit.

	; ../kitctl dep list
	io.gravwell.networkenrichment >= 2

### dep add

This subcommand adds a new dependency to the kit. Use the `-id` flag to specify the kit ID and the `-version` flag to give the required minimum version of that kit.

	; kitctl -id io.gravwell.networkenrichment -version 2 dep add

### dep del

This subcommand removes a dependency from the kit. Use the `-id` flag to specify the dependency to be removed.

	; ../kitctl -id io.gravwell.networkenrichment dep del