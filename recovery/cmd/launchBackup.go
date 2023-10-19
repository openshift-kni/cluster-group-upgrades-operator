/*
 * Copyright 2022 Red Hat, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *   http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package cmd

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/spf13/cobra"

	"os"
	"path/filepath"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/openshift-kni/cluster-group-upgrades-operator/recovery/generated"
)

const host string = "/host"
const recoveryScript string = "upgrade-recovery.sh"
const backupPath string = "/var/recovery"

// RecoveryInProgress checks if a restore is in progress
// returns:			bool
func RecoveryInProgress(backupPath string) bool {
	progressfile := filepath.Join(backupPath, "progress")

	if _, err := os.Stat(progressfile); os.IsNotExist(err) {
		return false
	}
	return true
}

// LaunchBackup triggers the backup procedure
// returns:			error
//
//nolint:gocritic
func LaunchBackup() error {

	var err error
	insufficientDiskSpaceError := errors.New("insufficient disk space to trigger backup")

	// Prepare the host directory for taking backups
	if err = InitBackup(); err != nil {
		log.Error("Failed to initialize the pre-requisite for taking a backup")
		return err
	}

	// change root directory to /host
	if err = syscall.Chroot(host); err != nil {
		log.Errorf("Couldn't do chroot to %s, err: %s", host, err)
		return err
	}

	// Handle "/run/ostree-booted" flag file by renaming it to "/run/ostree-booted.tmp"
	ostreeBooted := "/run/ostree-booted"
	ostreeBootedRenamed := ""
	// Check if ostree-booted file exists and is a flag-file (i.e. empty file)
	if info, err := os.Stat(ostreeBooted); err == nil && !info.IsDir() && info.Size() == 0 {
		ostreeBootedRenamed = ostreeBooted + ".tmp"
		if err = os.Rename(ostreeBooted, ostreeBootedRenamed); err == nil {
			log.Infof("Successfully renamed %s to %s\n", ostreeBooted, ostreeBootedRenamed)
		} else {
			log.Errorf("Failed to rename %s: %v\n", ostreeBooted, err)
		}
	}

	// Use a defer function to change "/run/ostree-booted" back to the original value before exiting
	defer func() error {
		if ostreeBootedRenamed != "" {
			if info, localErr := os.Stat(ostreeBootedRenamed); localErr == nil && !info.IsDir() && info.Size() == 0 {
				if localErr = os.Rename(ostreeBootedRenamed, ostreeBooted); localErr == nil {
					log.Infof("Successfully renamed %s back to %s\n", ostreeBootedRenamed, ostreeBooted)
				} else {
					log.Infof("Failed to rename %s back to %s: %v\n", ostreeBootedRenamed, ostreeBooted, localErr)
				}
			}
		}

		// Check "err" for insufficient disk space error, exit if it is
		if err == insufficientDiskSpaceError {
			os.Exit(1)
		}

		// return error that triggered the defer function
		return err
	}()

	// During recovery, this container may get relaunched, as it will be in "Running"
	// state when the backup is taken. We'll check to see if a recovery is already
	// in progress then, and just exit cleanly if so.
	if RecoveryInProgress(backupPath) {
		log.Info("Cannot take backup. Recovery is currently in progress")
		return nil
	}

	if err = os.Chdir("/"); err != nil {
		log.Error("Couldn't do chdir")
		return err
	}

	// validate path
	if _, err = os.Stat(backupPath); os.IsNotExist(err) {
		// create path
		err = os.Mkdir(backupPath, 0700)
		if err != nil {
			log.Error(err)
			return err
		}
	}

	err = Cleanup(backupPath)
	if err != nil {
		log.Errorf("Old directories couldn't be deleted, err: %s\n", err)
	}

	log.Info("Old contents have been cleaned up")

	// Verify disk space
	var ok bool
	ok, err = compareBackupToDisk()
	if err != nil {
		log.Error(err)
		return err
	}

	if !ok {
		err = insufficientDiskSpaceError
		log.Error(err)
		return err
	}

	log.Info("Sufficient disk space found to trigger backup")

	scriptname := filepath.Join(backupPath, recoveryScript)
	scriptcontent, _ := generated.Asset(recoveryScript)
	err = os.WriteFile(scriptname, scriptcontent, 0700)
	if err != nil {
		log.Error(err)
		return err
	}
	log.Info("Upgrade recovery script written")

	// Take backup
	backupCmd := fmt.Sprintf("%s --take-backup --dir %s", scriptname, backupPath)
	err = ExecuteCmd(backupCmd)
	if err != nil {
		return err
	}

	log.Info(strings.Repeat("-", 60))
	log.Info("backup has successfully finished ...")

	return nil

}

// InitBackup Prepare backup pre-requisites
// returns: 			error
func InitBackup() error {

	hostDev := filepath.Join(host, "dev")
	hostDevNull := filepath.Join(hostDev, "null")
	hostDevShm := filepath.Join(hostDev, "shm")
	hostSysroot := filepath.Join(host, "sysroot")

	// Create null device (/dev/null) on /host
	if err := os.MkdirAll(hostDev, 0o751); err != nil {
		log.Errorf("Failed to create %s directory, err: %s\n", hostDev, err)
		return err
	}

	// Create shm device (/dev/shm) on /host
	if err := os.MkdirAll(hostDevShm, 0o751); err != nil {
		log.Errorf("Failed to create %s directory, err: %s\n", hostDevShm, err)
		return err
	}

	if err := syscall.Mknod(hostDevNull, uint32(os.FileMode(0o666)), int(unix.Mkdev(uint32(1), uint32(3)))); err != nil {
		log.Errorf("Failed to create device %s, err: %s\n", hostDevNull, err)
		return err
	}

	// Mount /dev/shm
	flags := syscall.MS_NOEXEC | syscall.MS_NODEV | syscall.MS_NOSUID | syscall.MS_RELATIME
	if err := syscall.Mount("tmpfs", hostDevShm, "tmpfs", uintptr(flags), ""); err != nil {
		log.Errorf("Error mounting tmpfs: %v\n", err)
		return err
	}
	log.Infof("Successfully mounted %s\n", hostDevShm)

	// Remount host/sysroot for read-write access for executing ostree admin <undeploy|pin>
	if err := syscall.Mount(hostSysroot, hostSysroot, "", syscall.MS_REMOUNT, ""); err != nil {
		log.Errorf("Failed to remount %s directory, err: %s\n", hostSysroot, err)
		return err
	}
	log.Infof("Successfully remounted %s with r/w permission\n", hostSysroot)

	// Create symbolic links for running ostree commands in jailed root "host"
	if err := os.Chdir(host); err != nil {
		log.Error("Failed to chdir to /host/")
		return err
	}
	// Create symbolic link for usr/lib64 to lib64
	if err := syscall.Symlink("usr/lib64", "lib64"); err != nil {
		log.Errorf("Failed to create a link from /usr/lib64 to lib64 from chroot %s\n, err: %s", host, err)
		return err
	}
	// Create symbolic links for usr/bin to bin
	if err := syscall.Symlink("usr/bin", "bin"); err != nil {
		log.Error("Failed to create a link from /usr/bin to /bin")
		return err
	}

	// Create symbolic links for sysroot/ostree to ostree
	if err := syscall.Symlink("sysroot/ostree", "ostree"); err != nil {
		log.Error("Failed to create a link from /sysroot/ostree/ to /ostree")
		return err
	}

	if err := os.Chdir("/"); err != nil {
		log.Error("Failed to chdir to /")
		return err
	}

	return nil
}

// Cleanup deletes all old subdirectories and files in the recovery partition
// returns: 			error
func Cleanup(path string) error {
	log.Info(strings.Repeat("-", 60))
	log.Info("Cleaning up old content...")
	log.Info(strings.Repeat("-", 60))
	// Cleanup previous backups
	dir, _ := os.Open(path)
	subDir, _ := dir.Readdir(0)

	// Loop over the directory's files.
	for index := range subDir {
		fileNames := subDir[index]

		// Get name of file and its full path.
		name := fileNames.Name()
		fullPath := path + "/" + name
		log.Info("\nfullpath: ", fullPath)

		// Remove the file.
		err := os.RemoveAll(fullPath)
		if err != nil {
			log.Error(err)
			return err
		}
	}
	log.Info("Old directories deleted with contents")

	return nil
}

// ExecuteCmd execute shell commands
// returns: 			error
func ExecuteCmd(cmd string) error {

	logger := log.StandardLogger()
	lw := logger.Writer()

	log.Infof("Running: bash -c %s", cmd)
	execCmd := exec.Command("bash", "-c", cmd)

	execCmd.Stdout = lw
	execCmd.Stderr = lw

	err := execCmd.Run()

	lw.Close()

	if err != nil {
		log.Error(err)
		return err
	}

	return nil
}

// launchBackupCmd represents the launch command
var launchBackupCmd = &cobra.Command{
	Use:   "launchBackup",
	Short: "It will trigger backup of resources in the specified path",

	RunE: func(cmd *cobra.Command, args []string) error {
		// start launching the backup of the resource
		return LaunchBackup()
	},
}

func init() {

	rootCmd.AddCommand(launchBackupCmd)

}
