# ipexist
A library for efficiently storing and checking for the existence of an IPv4 set with high density sets.

## Purpose
The purpose of this library is to trade the size of the resulting set for efficiency in lookups.

For very sparse IP sets the memory footprint is innefficient, for very dense sets the footprint can be very efficient.
