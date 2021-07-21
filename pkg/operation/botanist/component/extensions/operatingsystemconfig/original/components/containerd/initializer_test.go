// Copyright (c) 2021 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package containerd_test

import (
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/gardener/gardener/pkg/operation/botanist/component/extensions/operatingsystemconfig/original/components"
	. "github.com/gardener/gardener/pkg/operation/botanist/component/extensions/operatingsystemconfig/original/components/containerd"
	"github.com/gardener/gardener/pkg/utils"
	"github.com/gardener/gardener/pkg/utils/imagevector"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"k8s.io/utils/pointer"
)

var _ = Describe("Initializer", func() {
	Describe("#Config", func() {
		var (
			component components.Component
			ctx       components.Context

			images = map[string]*imagevector.Image{
				"pause-container": {
					Name:       "pause-container",
					Repository: pauseContainerImageRepo,
					Tag:        pointer.String(pauseContainerImageTag),
				},
			}
		)

		BeforeEach(func() {
			component = NewInitializer()
			ctx.Images = images
		})

		It("should return the expected units and files", func() {
			units, files, err := component.Config(ctx)

			Expect(err).NotTo(HaveOccurred())
			Expect(units).To(ConsistOf(
				extensionsv1alpha1.Unit{
					Name:    "containerd-initializer.service",
					Command: pointer.String("start"),
					Enable:  pointer.Bool(true),
					Content: pointer.String(`[Unit]
Description=Containerd initializer
[Install]
WantedBy=multi-user.target
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/opt/bin/init-containerd`),
				},
			))
			Expect(files).To(ConsistOf(
				extensionsv1alpha1.File{
					Path:        "/opt/bin/init-containerd",
					Permissions: pointer.Int32(744),
					Content: extensionsv1alpha1.FileContent{
						Inline: &extensionsv1alpha1.FileContentInline{
							Encoding: "b64",
							Data:     utils.EncodeBase64([]byte(initScript)),
						},
					},
				},
				extensionsv1alpha1.File{
					Path:        "/etc/systemd/system/containerd.service.d/10-require-containerd-initializer.conf",
					Permissions: pointer.Int32(0644),
					Content: extensionsv1alpha1.FileContent{
						Inline: &extensionsv1alpha1.FileContentInline{
							Data: `[Unit]
After=containerd-initializer.service
Requires=containerd-initializer.service`,
						},
					},
				},
			))
		})
	})
})

const (
	pauseContainerImageRepo = "foo.io"
	pauseContainerImageTag  = "v1.2.3"
	initScript              = `#!/bin/bash

FILE=/etc/containerd/config.toml
if [ ! -s "$FILE" ]; then
  mkdir -p /etc/containerd
  containerd config default > "$FILE"
fi

# use injected image as sandbox image
sandbox_image_line="$(grep sandbox_image $FILE | sed -e 's/^[ ]*//')"
pause_image=` + pauseContainerImageRepo + `:` + pauseContainerImageTag + `
sed -i  "s|$sandbox_image_line|sandbox_image = \"$pause_image\"|g" $FILE

BIN_PATH=/var/bin/containerruntimes
mkdir -p $BIN_PATH

ENV_FILE=/etc/systemd/system/containerd.service.d/30-env_config.conf
if [ ! -f "$ENV_FILE" ]; then
  cat <<EOF | tee $ENV_FILE
[Service]
Environment="PATH=$BIN_PATH:$PATH"
EOF
  systemctl daemon-reload
fi
`
)
