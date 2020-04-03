package main

import (
    "bufio"
    "errors"
    "flag"
    "io/ioutil"
    "log"
    "net"
    "os"
    "os/user"
    "runtime"
    "strconv"
    "strings"
    "sync"
    "time"

    "github.com/mazlum/cdnstrip/cdn"

    "github.com/briandowns/spinner"
)

func init() {
    runtime.GOMAXPROCS(runtime.NumCPU()) // Run faster !
}

// Initialize global variables
var cdnRanges []*net.IPNet
var mutex sync.Mutex
var wg sync.WaitGroup
var validIP int
var invalidIP int
var cdnIP int
var s = spinner.New(spinner.CharSets[11], 100*time.Millisecond)

func main() {
    cacheFilePath := getCacheFilePath()

    thread := flag.Int("t", 1, "Number of threads")
    input := flag.String("i", "-", "Input [FileName|Stdin]")
    out := flag.String("o", "filtered.txt", "Output file name")
    skipCache := flag.Bool("s", false, "Skip loading cache file for CDN IP ranges")
    flag.Parse()

    if *input == "" {
        flag.PrintDefaults()
        os.Exit(1)
    }

    // Start spinner
    print("\n")
    s.Writer = os.Stdout
    s.Start()

    // First check if cache exists
    s.Suffix = " Loading for cache file..."
    cahceFile, err := ioutil.ReadFile(cacheFilePath)
    if err == nil || *skipCache {
        // read cache file
        c := strings.Split(string(cahceFile), "\n")
        if len(c) == 0 {
            fatal(errors.New("empty cache file"))
        }
        for _, i := range c {
            _, cidr, err := net.ParseCIDR(i)
            if err == nil {
                // append range
                cdnRanges = append(cdnRanges, cidr)
            }
        }
    } else {
        // Create new cache file
        s.Suffix = " Loading all CDN ranges..."
        ranges, err := cdn.LoadAll()
        fatal(err)
        cdnRanges = ranges

        s.Suffix = " Creating new cache file..."
        cahceFile, err := os.OpenFile(cacheFilePath, os.O_TRUNC|os.O_RDWR|os.O_CREATE, 0664)
        fatal(err)
        for i, r := range cdnRanges {
            cahceFile.WriteString(r.String())
            if i != len(cdnRanges)-1 {
                cahceFile.WriteString("\n")
            }
        }
        cahceFile.Close()
    }

    outFile, err := os.Create(*out)
    fatal(err)
    defer outFile.Close()

    channel := make(chan string, *thread*2)
    for i := 0; i < *thread; i++ {
        wg.Add(1)
        go strip(channel, outFile)
    }

    loadInput(*input, channel)
    close(channel)
    wg.Wait()

    s.Stop()
    print("[✔]" + s.Suffix + "\n")
}

func strip(channel chan string, file *os.File) {
    defer wg.Done()
    for ip := range channel {
        i := net.ParseIP(ip)
        if i != nil {
            if cdn.Check(cdnRanges, i) {
                mutex.Lock()
                cdnIP++
                mutex.Unlock()
            } else {
                mutex.Lock()
                validIP++
                file.WriteString(i.String() + "\n")
                mutex.Unlock()
            }
        } else {
            mutex.Lock()
            invalidIP++
            mutex.Unlock()
        }

        // Update spinner
        updateSpinnerStats()

    }
}

func updateSpinnerStats() {
    mutex.Lock()
    s.Suffix = "  [ VALID: " + strconv.Itoa(validIP) + " | INVALID: " + strconv.Itoa(invalidIP) + " | CDN: " + strconv.Itoa(cdnIP) + " ]"
    mutex.Unlock()
}

func getCacheFilePath() string {
    usr, err := user.Current()
    if err != nil {
        fatal(err)
    }
    return usr.HomeDir + "/.config/cdnstrip.cache"
}

func loadInput(param string, inputChan chan<- string) {
    s.Suffix = " Loading input..."
    var sc *bufio.Scanner
    if param == "-" {
        sc = bufio.NewScanner(os.Stdin)
    } else {
        f, err := os.Open(param)
        if err != nil {
            fatal(err)
        }
        defer f.Close()
        sc = bufio.NewScanner(f)
    }

    for sc.Scan() {
        line := strings.TrimSpace(sc.Text())
        if line == "" {
            continue
        }
        if ip := net.ParseIP(line); ip != nil {
            inputChan <- ip.String()
        } else if cidr, err := cdn.ExpandCIDR(line); err == nil {
            for _, ip := range cidr {
                inputChan <- ip
            }
        }
    }
}

func fatal(err error) {
    if err != nil {
        s.Stop()
        pc, _, _, ok := runtime.Caller(1)
        details := runtime.FuncForPC(pc)
        if ok && details != nil {
            log.Printf("[%s] ERROR: %s\n", strings.ToUpper(strings.Split(details.Name(), ".")[1]), err)
        } else {
            log.Printf("[UNKOWN] ERROR: %s\n", err)
        }
        os.Exit(1)
    }
}
