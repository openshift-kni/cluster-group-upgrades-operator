package cmd

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	log "github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
)

var (
	clusterPath         = "/var/lib/etcd/member/snap/db"
	staticPodsPath      = "/etc/kubernetes/static-pod-resources/"
	usrLocalPath        = "/usr/local"
	kubeletPath         = "/var/lib/kubelet"
	etcPath             = "/etc"
	binPath             = "/etc/kubernetes/static-pod-resources/bin/"
	backupDir           = "/var/recovery"
	backupSizeSafetyNet = 10.00 // in GiB
	units               = []string{"B", "KiB", "MiB", "GiB", "TiB", "EiB", "ZiB"}
)

type resource struct {
	dirPath string
}

type resourceList struct {
	resources *[]resource
}

// compareBackupToDisk verifies disk space required against available
// disk space and returns boolean value
func compareBackupToDisk() (bool, error) {

	estimated, err := estimateDiskSize()
	if err != nil {
		log.Errorf("Couldn't calculate estimated disk space")
		return false, err
	}

	freeDisk, err := diskPartitionSize()
	if err != nil {
		log.Errorf("Couldn't calculate free disk space")
		return false, err
	}

	estimated, ebytes := sizeConversion(estimated)
	freeDisk, fbytes := sizeConversion(freeDisk)

	log.Infof("Available disk space : %.2f %s; Estimated disk space required for backup: %.2f %s \n", freeDisk, fbytes, estimated, ebytes)

	// find the index of estimated and freedisk space in units[]
	var ebytesID, fbytesID int
	for i := range units {
		if units[i] == ebytes {
			ebytesID = i
		}
		if units[i] == fbytes {
			fbytesID = i
		}
	}

	if ebytesID <= fbytesID {
		x := float64(fbytesID - ebytesID)
		if x != 0.0 {
			estimated /= 1024 * x
		}
		if freeDisk > backupSizeSafetyNet+estimated {
			return true, nil
		}
	}
	return false, nil
}

// estimateDiskSize calculate the required backup size
// returns: disk size(int), error
func estimateDiskSize() (float64, error) {

	DirList := resourceList{
		&[]resource{{staticPodsPath},
			{clusterPath},
			{usrLocalPath},
			{kubeletPath},
			{etcPath},
		},
	}

	estDirMap := map[string]float64{}
	var total float64

	for _, v := range *DirList.resources {
		_, err := os.Lstat(v.dirPath)
		if err != nil {
			return total, err
		}

		switch v.dirPath {
		case staticPodsPath:
			binPathSize := dirSize(binPath)
			estDirMap[v.dirPath] = dirSize(v.dirPath) - binPathSize

		case clusterPath:
			estDirMap[v.dirPath] = dirSize(v.dirPath) + estDirMap[staticPodsPath]

		default:
			estDirMap[v.dirPath] = dirSize(v.dirPath)
		}

		total += estDirMap[v.dirPath]

	}
	return total, nil
}

// diskPartitionSize calculate current disk space
// returns:     disk size(int), error
func diskPartitionSize() (float64, error) {

	var (
		stat          unix.Statfs_t
		freeDiskSpace float64
	)

	err := unix.Statfs(backupDir, &stat)
	if err != nil {
		return freeDiskSpace, err
	}

	freeDiskSpace = float64(stat.Bavail * uint64(stat.Bsize))

	err = unix.Statfs("/", &stat)
	if err != nil {
		return freeDiskSpace, err
	}
	total, tbytes := sizeConversion(float64(stat.Blocks * uint64(stat.Bsize)))
	log.Infof("Total disk size is: %.2f %s", total, tbytes)

	return freeDiskSpace, nil
}

// dirSize calculates current diskspace used by the files under a directory
// returns:    size of the directory(float64)
func dirSize(path string) float64 {
	var size float64
	var dirs []string

	err := filepath.Walk(path, func(dirPath string, info os.FileInfo, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrPermission) {
				dirs = append(dirs, dirPath)
			}
			return filepath.SkipDir
		}
		if !info.IsDir() {
			if info.Name() != binPath {
				size += float64(info.Size())
			}

		}
		return err
	})

	if len(dirs) != 0 {
		log.Warnf("\nCouldn't fetch disk size for below \ndirectories due to permission denied errors :  \n%s \n", dirs)
	}

	if err != nil {
		size = 0.0
	}

	return size
}

// sizeConversion coverts the bytes into its multiple
// returns:  converted size(int), corresponding metric(string)
func sizeConversion(size float64) (float64, string) {
	i := 0
	if size >= 1024 {
		for i < len(units) && size >= 1024 {
			i++
			size /= 1024
		}
	}

	return size, units[i]
}
