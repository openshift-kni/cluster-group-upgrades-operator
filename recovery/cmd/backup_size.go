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
	backupSizeSafetyNet = 10.00 * 1024 * 1024 * 1024 // in GiB
	units               = []string{"B", "KiB", "MiB", "GiB", "TiB", "EiB", "ZiB"}
)

type resource struct {
	dirPath string
}

type resourceList struct {
	resources *[]resource
}

// compareBackupToDisk verifies disk space required against available disk space
// returns: boolean, error
func compareBackupToDisk() (bool, error) {

	estimated, err := EstimateFsSpaceRequirements()
	if err != nil {
		log.Errorf("Couldn't calculate estimated disk space required for backup")
		return false, err
	}

	freeDisk, err := DiskPartitionSize()
	if err != nil {
		log.Errorf("Couldn't calculate free disk space")
		return false, err
	}

	estimatedSizeConverted, ebytes := SizeConversion(estimated)
	freeDiskSizeConverted, fbytes := SizeConversion(freeDisk)

	log.Infof("Available disk space : %.2f %s; Estimated disk space required for backup: %.2f %s \n", freeDiskSizeConverted, fbytes, estimatedSizeConverted, ebytes)

	return Compare(freeDisk, estimated), nil
}

// Compare verifies freedisk against estimated and safetyNet calculation
// returns: boolean
func Compare(freeDisk, estimated float64) bool {

	if freeDisk > estimated+backupSizeSafetyNet {
		return true
	}
	return false
}

// EstimateFsSpaceRequirements calculate the required backup size
// returns: disk size(float64), error
func EstimateFsSpaceRequirements() (float64, error) {

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
			binPathSize := DirSize(binPath)
			estDirMap[v.dirPath] = DirSize(v.dirPath) - binPathSize

		case clusterPath:
			estDirMap[v.dirPath] = DirSize(v.dirPath) + estDirMap[staticPodsPath]

		default:
			estDirMap[v.dirPath] = DirSize(v.dirPath)
		}

		total += estDirMap[v.dirPath]

	}
	return total, nil
}

// DiskPartitionSize calculate current disk space
// returns:     disk size(float64), error
func DiskPartitionSize() (float64, error) {

	var (
		stat          unix.Statfs_t
		freeDiskSpace float64
	)

	err := unix.Statfs(backupDir, &stat)
	if err != nil {
		return freeDiskSpace, err
	}

	freeDiskSpace = float64(stat.Bavail * uint64(stat.Bsize))

	return freeDiskSpace, nil
}

// DirSize calculates current diskspace used by the files under a directory
// returns:    size of the directory(float64)
func DirSize(path string) float64 {
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

// SizeConversion coverts the bytes into its multiple
// returns:  converted size(float64), corresponding metric(string)
func SizeConversion(size float64) (float64, string) {
	i := 0
	if size >= 1024 {
		for i < len(units) && size >= 1024 {
			i++
			size /= 1024
		}
	}

	return size, units[i]
}
