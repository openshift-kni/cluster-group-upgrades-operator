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
	"fmt"
	"os/exec"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"os"
	"path/filepath"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/openshift-kni/cluster-group-upgrades-operator/recovery/generated"
)

const host string = "/host"
const recoveryScript string = "upgrade-recovery.sh"

//RecoveryInProgress checks if a restore is in progress
// returns:			bool
func RecoveryInProgress(BackupPath string) bool {
	progressfile := filepath.Join(BackupPath, "progress")

	if _, err := os.Stat(progressfile); os.IsNotExist(err) {
		return false
	}
	return true
}

//ParseBackupPath parses the BackupPath
// returns:			string
func ParseBackupPath(BackupPath string) string {
	if check := strings.Contains(BackupPath[len(BackupPath)-1:], "/"); check {
		BackupPath = BackupPath[:len(BackupPath)-1]
	}
	return BackupPath
}

//LaunchBackup triggers the backup procedure
// returns:			error
func LaunchBackup(BackupPath string) error {

	// check for slash in the BackupPath
	BackupPath = ParseBackupPath(BackupPath)

	//change root directory to /host
	if err := syscall.Chroot(host); err != nil {
		log.Errorf("Couldn't do chroot to %s, err: %s", host, err)
		return err
	}

	// During recovery, this container may get relaunched, as it will be in "Running"
	// state when the backup is taken. We'll check to see if a recovery is already
	// in progress then, and just exit cleanly if so.
	if RecoveryInProgress(BackupPath) {
		log.Info("Cannot take backup. Recovery is currently in progress")
		return nil
	}

	if err := os.Chdir("/"); err != nil {
		log.Error("Couldn't do chdir")
		return err
	}

	// validate path
	if _, err := os.Stat(BackupPath); os.IsNotExist(err) {
		// create path
		err := os.Mkdir(BackupPath, 0700)
		if err != nil {
			log.Error(err)
			return err
		}
	}

	err := Cleanup(BackupPath)
	if err != nil {
		log.Errorf("Old directories couldn't be deleted, err: %s\n", err)
	}

	log.Info("Old contents have been cleaned up")

	scriptname := filepath.Join(BackupPath, recoveryScript)
	scriptcontent, _ := generated.Asset(recoveryScript)
	err = os.WriteFile(scriptname, scriptcontent, 0700)
	if err != nil {
		log.Error(err)
		return err
	}
	log.Info("Upgrade recovery script written")

	// Take backup
	backupCmd := fmt.Sprintf("%s --take-backup --dir %s", scriptname, BackupPath)
	err = ExecuteCmd(backupCmd)
	if err != nil {
		return err
	}

	log.Info(strings.Repeat("-", 60))
	log.Info("backup has successfully finished ...")

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

//ExecuteCmd execute shell commands
//returns: 			error
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
		BackupPath, _ := cmd.Flags().GetString("BackupPath")

		// start launching the backup of the resource
		return LaunchBackup(BackupPath)
	},
}

func init() {

	rootCmd.AddCommand(launchBackupCmd)

	launchBackupCmd.Flags().StringP("BackupPath", "p", "", "Path where to store the backup")
	_ = launchBackupCmd.MarkFlagRequired("BackupPath")

	// bind to viper
	_ = viper.BindPFlag("BackupPath", launchBackupCmd.Flags().Lookup("BackupPath"))
}
