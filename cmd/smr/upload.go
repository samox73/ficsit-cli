package smr

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/satisfactorymodding/ficsit-cli/cli"
	"github.com/satisfactorymodding/ficsit-cli/ficsit"
)

const uploadVersionPartGQL = `mutation UploadVersionPart($modId: ModID!, $versionId: VersionID!, $part: Int!, $file: Upload!) {
  uploadVersionPart(modId: $modId, versionId: $versionId, part: $part, file: $file)
}`

func init() {
	Cmd.AddCommand(uploadCmd)
}

var uploadCmd = &cobra.Command{
	Use:   "upload [flags] <mod-id> <file> <changelog...>",
	Short: "Upload a new mod version",
	Args:  cobra.MinimumNArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		chunkSize := viper.GetInt64("chunk-size")
		if chunkSize < 1000000 {
			return errors.New("chunk size cannot be smaller than 1MB")
		}

		var versionStability ficsit.VersionStabilities
		switch viper.GetString("stability") {
		case "alpha":
			versionStability = ficsit.VersionStabilitiesAlpha
		case "beta":
			versionStability = ficsit.VersionStabilitiesBeta
		case "release":
			versionStability = ficsit.VersionStabilitiesRelease
		default:
			return errors.New("invalid version stability: " + viper.GetString("stability"))
		}

		modID := args[0]
		filePath := args[1]
		changelog := strings.Join(args[2:], " ")

		stat, err := os.Stat(filePath)
		if err != nil {
			return errors.Wrap(err, "failed to stat file")
		}

		global, err := cli.InitCLI(true)
		if err != nil {
			return err
		}

		if stat.IsDir() {
			return errors.New("file cannot be a directory")
		}

		// TODO Validate .smod file before upload

		logBase := log.With().Str("mod-id", modID).Str("path", filePath).Logger()
		logBase.Info().Msg("creating a new mod version")

		createdVersion, err := ficsit.CreateVersion(cmd.Context(), global.APIClient, modID)
		if err != nil {
			return err
		}

		logBase = logBase.With().Str("version-id", createdVersion.GetVersionID()).Logger()
		logBase.Info().Msg("received version id")

		// TODO Parallelize chunk uploading
		chunkCount := int(math.Ceil(float64(stat.Size()) / float64(chunkSize)))
		for i := 0; i < chunkCount; i++ {
			chunkLog := logBase.With().Int("chunk", i).Logger()
			chunkLog.Info().Msg("uploading chunk")

			f, err := os.Open(filePath)
			if err != nil {
				return errors.Wrap(err, "failed to open file")
			}

			offset := int64(i) * chunkSize
			if _, err := f.Seek(offset, 0); err != nil {
				return errors.Wrap(err, "failed to seek to chunk offset")
			}

			bufferSize := chunkSize
			if offset+chunkSize > stat.Size() {
				bufferSize = stat.Size() - offset
			}

			chunk := make([]byte, bufferSize)
			if _, err := f.Read(chunk); err != nil {
				return errors.Wrap(err, "failed to read from chunk offset")
			}

			operationBody, err := json.Marshal(map[string]interface{}{
				"query": uploadVersionPartGQL,
				"variables": map[string]interface{}{
					"modId":     modID,
					"versionId": createdVersion.GetVersionID(),
					"part":      i + 1,
					"file":      nil,
				},
			})
			if err != nil {
				return errors.Wrap(err, "failed to serialize operation body")
			}

			mapBody, err := json.Marshal(map[string]interface{}{
				"0": []string{"variables.file"},
			})
			if err != nil {
				return errors.Wrap(err, "failed to serialize map body")
			}

			body := &bytes.Buffer{}
			writer := multipart.NewWriter(body)

			operations, err := writer.CreateFormField("operations")
			if err != nil {
				return errors.Wrap(err, "failed to create operations field")
			}

			if _, err := operations.Write(operationBody); err != nil {
				return errors.Wrap(err, "failed to write to operation field")
			}

			mapField, err := writer.CreateFormField("map")
			if err != nil {
				return errors.Wrap(err, "failed to create map field")
			}

			if _, err := mapField.Write(mapBody); err != nil {
				return errors.Wrap(err, "failed to write to map field")
			}

			part, err := writer.CreateFormFile("0", filepath.Base(filePath))
			if err != nil {
				return errors.Wrap(err, "failed to create file field")
			}

			if _, err := io.Copy(part, bytes.NewReader(chunk)); err != nil {
				return errors.Wrap(err, "failed to write to file field")
			}

			if err := writer.Close(); err != nil {
				return errors.Wrap(err, "failed to close body writer")
			}

			r, _ := http.NewRequest("POST", viper.GetString("api-base")+viper.GetString("graphql-api"), body)
			r.Header.Add("Content-Type", writer.FormDataContentType())
			r.Header.Add("Authorization", viper.GetString("api-key"))

			client := &http.Client{}
			if _, err := client.Do(r); err != nil {
				return errors.Wrap(err, "failed to execute request")
			}

		}

		logBase.Info().Msg("finalizing uploaded version")

		finalizeSuccess, err := ficsit.FinalizeCreateVersion(cmd.Context(), global.APIClient, modID, createdVersion.GetVersionID(), ficsit.NewVersion{
			Changelog: changelog,
			Stability: versionStability,
		})
		if err != nil {
			return err
		}

		if !finalizeSuccess.GetSuccess() {
			logBase.Error().Msg("failed to finalize version upload")
		}

		time.Sleep(time.Second * 1)

		for {
			logBase.Info().Msg("checking version upload state")
			state, err := ficsit.CheckVersionUploadState(cmd.Context(), global.APIClient, modID, createdVersion.GetVersionID())
			if err != nil {
				logBase.Err(err).Msg("failed to upload mod")
				return nil
			}

			if state == nil || state.GetState().Version.Id == "" {
				time.Sleep(time.Second * 10)
				continue
			}

			if state.GetState().Auto_approved {
				logBase.Info().Msg("version successfully uploaded and auto-approved")
				break
			}

			logBase.Info().Msg("version successfully uploaded, but has to be scanned for viruses, which may take up to 15 minutes")
			break
		}

		return nil
	},
}

func init() {
	uploadCmd.PersistentFlags().Int64("chunk-size", 10000000, "Size of chunks to split uploaded mod in bytes")
	uploadCmd.PersistentFlags().String("stability", "release", "Stability of the uploaded mod (alpha, beta, release)")

	_ = viper.BindPFlag("chunk-size", uploadCmd.PersistentFlags().Lookup("chunk-size"))
	_ = viper.BindPFlag("stability", uploadCmd.PersistentFlags().Lookup("stability"))
}
