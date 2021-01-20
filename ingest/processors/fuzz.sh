#!/bin/bash

set -e
go get github.com/dvyukov/go-fuzz/go-fuzz
go get github.com/dvyukov/go-fuzz/go-fuzz-build


rm -rf /dev/shm/fuzzing
rm -f /dev/shm/fuzz_bin.zip
case $1 in
	ise_parser)
		mkdir /dev/shm/fuzzing
		mkdir /dev/shm/fuzzing/corpus
		cp -r fuzz_corpus/cisco_ise/parse/* /dev/shm/fuzzing/corpus/
		go-fuzz-build -o=/dev/shm/fuzz_bin.zip -func=FuzzCiscoISEParser .
		echo "fuzzing ise_parser"
		go-fuzz -workdir=/dev/shm/fuzzing -bin=/dev/shm/fuzz_bin.zip
		;;
	ise_assembler)
		mkdir /dev/shm/fuzzing
		mkdir /dev/shm/fuzzing/corpus
		cp -r fuzz_corpus/cisco_ise/reassemble/* /dev/shm/fuzzing/corpus/
		go-fuzz-build -o=/dev/shm/fuzz_bin.zip -func=FuzzCiscoISEAssembler .
		echo "ise_assembler"
		go-fuzz -workdir=/dev/shm/fuzzing -bin=/dev/shm/fuzz_bin.zip
		;;
	*)
		echo "unknown fuzz target"
		;;
esac
