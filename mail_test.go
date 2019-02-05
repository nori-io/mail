// Copyright (C) 2018 The Nori Authors info@nori.io
//
// This program is free software; you can redistribute it and/or
// modify it under the terms of the GNU Lesser General Public
// License as published by the Free Software Foundation; either
// version 3 of the License, or (at your option) any later version.
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the GNU
// Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with this program; if not, see <http://www.gnu.org/licenses/>.
package main

import (
	"testing"
	"time"

	"github.com/golang/protobuf/ptypes"
	"github.com/stretchr/testify/assert"

	"github.com/nori-io/nori-common/interfaces"
	"github.com/nori-io/nori-plugins/mail/message"
	"github.com/nori-io/nori/core/config"
	"github.com/nori-io/nori/core/plugins"
	"github.com/nori-io/nori/core/plugins/mocks"
)

const (
	testTemplate     = "TestTemplate"
	testTemplateBody = "{{ .Test }}"
)

func TestPackage(t *testing.T) {
	assert := assert.New(t)

	registry := new(mocks.Registry)

	cfg := config.Config
	cfg.SetDefault("mail.host", "")
	cfg.SetDefault("mail.port", 0)
	cfg.SetDefault("mail.user", "")
	cfg.SetDefault("mail.password", "")
	cfg.SetDefault("mail.ssl", false)
	cfg.SetDefault("mail.worker.enabled", true)
	cfg.SetDefault("mail.worker.pool_size", 1)

	registry.On("Config").Return(cfg)
	registry.On("Logger").Return(config.Log)

	pm := plugins.GetPluginManager(nil)

	pm.Load("../plugins")

	pubsubPlugin := pm.Plugins()["pubsub"].Plugin()
	assert.NotNil(pubsubPlugin)

	err := pubsubPlugin.Start(nil, registry)
	assert.Nil(err)

	pubsub := pubsubPlugin.GetInstance().(interfaces.PubSub)

	registry.On("PubSub").Return(pubsub)

	templatesPlugin := pm.Plugins()["templates"].Plugin()
	assert.NotNil(templatesPlugin)

	err = templatesPlugin.Start(nil, registry)
	assert.Nil(err)

	templates := templatesPlugin.GetInstance().(interfaces.Templates)

	registry.On("Templates").Return(templates)

	templates.Set(testTemplate, testTemplateBody)

	p := new(plugin)

	assert.NotNil(p.Meta())
	assert.NotEmpty(p.Meta().GetDescription().Name)

	p.Start(nil, registry)

	mail, ok := p.Instance().(interfaces.Mail)
	assert.True(ok)
	assert.NotNil(mail)

	msg := &message.Message{
		ID:           1,
		UserID:       1,
		Email:        "test@example.com",
		TracingID:    1,
		Timestamp:    ptypes.TimestampNow(),
		TTL:          ptypes.DurationProto(time.Minute * 30),
		ServiceName:  "TestService",
		TemplateName: testTemplate,
		Variables: map[string]string{
			"Test":       "Test",
			subjectKey:   "test",
			fromEmailKey: "test@example.com",
			fromNameKey:  "test",
		},
	}

	err = mail.Send(msg)
	assert.Nil(err)

	time.Sleep(time.Second * 10)

	err = p.Stop(nil, nil)
	assert.Nil(err)
	assert.Nil(p.Instance())
}
