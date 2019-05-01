/*
Serve starts a simple temporary static file server in your current directory and prints your IP address to share with colleagues.

Based on the Gist https://gist.github.com/paulmach/7271283/2a1116ca15e34ee23ac5a3a87e2a626451424993
by Paul Mach (https://github.com/paulmach)

Usage:
  -a string
        Require basic authentication with the given credentials (e.g. -a "alice:secret")
  -d string
        The directory of static files to host (default ".")
  -h    Print the usage
  -p string
        Port to serve on. 8080 by default for HTTP, 8443 for HTTPS (when using the -s flag) (default "8080")
  -s    Serve via HTTPS instead of HTTP. Creates a temporary self-signed certificate for localhost, 127.0.0.1, <hostname>.local, <hostname>.lan, <hostname>.home and the determined LAN IP address
  -t    Test / dry run (just prints the interface table)
  -v    Print the version

Navigating to http://localhost:8080 will display the index.html or directory listing file.
*/
package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
)

// Increment and remove "+" in release commits.
// Add "+" after release commits.
const version = "v0.2.1"

// Flags in alphabetical order, just like "-h" prints them
var (
	auth         = flag.String("a", "", `Require basic authentication with the given credentials (e.g. -a "alice:secret")`)
	directory    = flag.String("d", ".", "The directory of static files to host")
	help         = flag.Bool("h", false, "Print the usage")
	port         = flag.String("p", "8080", "Port to serve on. 8080 by default for HTTP, 8443 for HTTPS (when using the -s flag)")
	https        = flag.Bool("s", false, "Serve via HTTPS instead of HTTP. Creates a temporary self-signed certificate for localhost, 127.0.0.1, <hostname>.local, <hostname>.lan, <hostname>.home and the determined LAN IP address")
	test         = flag.Bool("t", false, "Test / dry run (just prints the interface table)")
	printVersion = flag.Bool("v", false, "Print the version")
)

func main() {
	flag.Parse()

	// If the "-h" flag was used, only print the usage and exit.
	// Without the "-h" flag in the flag definitions "-h" would also print the usage,
	// but only because it's an unknown flag and then exit with exit code 2 (instead of 0).
	if *help {
		// flag.Usage() prints to stderr by default because it's usually called internally
		// (in the flag package) when an invalid argument is used.
		// But in our case it's meant to be printed, so print to stdout.
		flag.CommandLine.SetOutput(os.Stdout)
		flag.Usage()
		os.Exit(0)
	}

	// If the "-v" flag was used, only print the version and exit
	if *printVersion {
		fmt.Printf("serve version: %v\n", version)
		os.Exit(0)
	}

	// Precondition checks.
	// Should be done for most string flags.

	// Auth
	if *auth != "" && !strings.Contains(*auth, ":") {
		log.Fatal(`When using the -a flag to add basic authentication, you must specify credentials in the form of "username:password". For example: "alice:secret".`)
	}
	// Directory
	// Use Stat instead of Lstat because serving works with softlinks (e.g. "serve -d ./softlink")
	// Note: All of the following checks aren't really necessary, but user-friendly.
	// Not necessary because the dir must only exist and be a dir and be readable on each request.
	// So theoretically the server could already be started and the directory created/modified later.
	fileInfo, err := os.Stat(*directory)
	if err != nil {
		log.Fatalf("%v can't be served: %v\n", *directory, err)
	} else if !fileInfo.IsDir() {
		log.Fatalf("%v can't be served because it's a file and not a directory: %v\n", *directory, err)
	} else {
		file, err := os.Open(*directory)
		if err != nil {
			log.Fatalf("%v can't be served because it's not readable: %v\n", *directory, err)
		} else {
			file.Close() // Ignore errors
		}
	}
	// Port: Will be detected by a lookup when calling ListenAndServe() at the end

	scheme := "http"
	if *https {
		scheme += "s"
		// If the port wasn't specifically set by the user,
		// change it from its default value 8080 to 8443 as mentioned in the flag description.
		if !isFlagPassed("p") {
			*port = "8443"
		}
	}

	// If the "-t" flag was used, only print the network interface table and exit
	if *test {
		printAddrs(scheme, *port)
		os.Exit(0)
	}

	finalHandler := http.FileServer(http.Dir(*directory))

	// If the "-a" flag was used, use basic authentication middleware
	if *auth != "" {
		finalHandler = withBasicAuth(finalHandler)
	}

	// Register handler for "/" in Go's DefaultServeMux
	http.Handle("/", finalHandler)

	fmt.Printf("\nServing \"%s\" on all network interfaces (0.0.0.0) on %v port: %s\n", *directory, strings.ToUpper(scheme), *port)

	// Print local network interfaces and their IP addresses
	printAddrs(scheme, *port)

	if *https {
		cert, sans, err := generateCert()
		if err != nil {
			log.Fatalf("Couldn't generate TLS certificates: %v\n", err)
		}
		tlsConfig := &tls.Config{
			MinVersion:               tls.VersionTLS12,
			PreferServerCipherSuites: true,
			Certificates:             []tls.Certificate{cert},
		}
		server := &http.Server{
			Addr:      ":" + *port,
			TLSConfig: tlsConfig,
		}
		fmt.Printf("\nTemporary self signed certificate valid for %v days for the following hostnames: %v\n", certValidityDurationDays, sans)
		// TODO: Print the certificate fingerprint so the server can send it to the client via a secure channel
		// and the client can then validate it to make sure it's not a different certificate created by a malicious actor (MitM).
		// But first do some research regarding if the fingerprint is sufficient for that.
		// Also the fingerprint should probably be a different one with each certificate generation,
		// which didn't seem to be the case during my trials, but maybe I generated the fingerprint in the wrong way.
		log.Fatal(server.ListenAndServeTLS("", ""))
	} else {

		log.Fatal(http.ListenAndServe(":"+*port, nil))
	}
}

// printAddrs prints the local network interfaces and their IP addresses
func printAddrs(scheme, port string) {
	fmt.Println("\nLocal network interfaces and their IP addresses so you can pass one to your colleagues:")
	ifaces, err := net.Interfaces()
	if err != nil {
		log.Fatal(err)
	}
	// We want the interface + IP address list to look like a table with a width of 80:
	//       Interface      |  IPv4 Address   |              IPv6 Address
	// ---------------------|-----------------|----------------------------------------
	// vEthernet (Standan.. | 192.168.178.123 | 1a23:850a:bf55:39a9:6dae:c378:9deb:5aff
	fmt.Println("      Interface      |  IPv4 Address   |              IPv6 Address              ")
	fmt.Println("---------------------|-----------------|----------------------------------------")
	fav := ""
	for _, iface := range ifaces {
		fmt.Printf("%-20v |", cutString(iface.Name, 20))

		// Select IPv4 and IPv6 address
		ipv4, ipv6 := getAddressesFromIface(iface)

		// If there's no favorite IPv4 address yet, check if we should pick the current one
		if fav == "" && isFav(iface) {
			fav = ipv4
		}

		fmt.Printf(" %-15v | %v\n", ipv4, ipv6)
	}

	// Show probable favorite
	if fav != "" {
		fmt.Printf("\nYou probably want to share:\n%v://%v:%v\n", scheme, fav, port)
	}
}
