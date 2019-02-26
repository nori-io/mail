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
	"context"
	"testing"
	"time"

	"github.com/golang/protobuf/ptypes"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/nori-io/mail/message"
	cfg "github.com/nori-io/nori-common/config"
	"github.com/nori-io/nori-common/interfaces"
	"github.com/nori-io/nori-common/mocks"
	"github.com/nori-io/nori/core/plugins"
	"github.com/nori-io/nori/version"
)

const (
	testTemplate     = "TestTemplate"
	testTemplateBody = "{{ .Test }}"

	pathPubsubPlugin      = "../plugins/pubsub.so"
	pathToTemplatesPlugin = "../plugins/templates.so"
)

func TestPackage(t *testing.T) {
	a := assert.New(t)
	ctx := context.Background()

	cfgManager := new(mocks.Manager)
	config := new(mocks.Config)
	logger := logrus.New()

	// set configuration
	cfgManager.On("Register", mock.Anything).Return(config)
	config.On("String", "mail.host", mock.Anything).Return(stringFunc(""))
	config.On("Int", "mail.port", mock.Anything).Return(intFunc(0))
	config.On("String", "mail.user", mock.Anything).Return(stringFunc(""))
	config.On("String", "mail.password", mock.Anything).Return(stringFunc(""))
	config.On("Bool", "mail.ssl", mock.Anything).Return(boolFunc(false))
	config.On("String", "mail.local_name", mock.Anything).Return(stringFunc(""))
	config.On("Bool", "mail.worker.enabled", mock.Anything).Return(boolFunc(true))
	config.On("Int", "mail.worker.pool_size", mock.Anything).Return(intFunc(1))
	config.On("String", "mail.key", mock.Anything).Return(stringFunc(""))
	config.On("String", subjectFromConfig, mock.Anything).Return(stringFunc(""))
	config.On("String", fromEmailFromConfig, mock.Anything).Return(stringFunc(""))
	config.On("String", fromNameFromConfig, mock.Anything).Return(stringFunc(""))

	config.On("String", "template.dir", mock.Anything).Return(stringFunc("../templates"))
	config.On("Bool", "template.watcher", mock.Anything).Return(boolFunc(false))
	config.On("Bool", "template.save_in_file", mock.Anything).Return(boolFunc(false))

	// mocking registry
	registry := new(mocks.Registry)
	registry.On("Logger", mock.Anything).Return(logger)

	// create temp plugin manager with nil storage
	v := version.NoriVersion(logger)
	pluginManager := plugins.NewManager(nil, cfgManager, v, logger)

	// need init and start plugin before getting instance
	plug, err := pluginManager.AddFile(pathPubsubPlugin)
	a.Nil(err)

	err = plug.Init(ctx, cfgManager)
	a.Nil(err)

	err = plug.Start(ctx, registry)
	a.Nil(err)

	pubsubInterface := plug.Instance()

	plug, err = pluginManager.AddFile(pathToTemplatesPlugin)
	a.Nil(err)

	err = plug.Init(ctx, cfgManager)
	a.Nil(err)

	err = plug.Start(ctx, registry)
	a.Nil(err)

	templateInterface := plug.Instance()

	// set instances of plugin to mocking registry
	registry.On("PubSub").Return(pubsubInterface, nil)
	registry.On("Templates").Return(templateInterface, nil)

	// don't forget to create test data
	err = templateInterface.(interfaces.Templates).Set(testTemplate, testTemplateBody)
	a.Nil(err)

	p := new(plugin)
	a.NotNil(p.Meta())

	err = p.Init(ctx, cfgManager)
	a.Nil(err)

	err = p.Start(ctx, registry)
	a.Nil(err)

	mail, ok := p.Instance().(interfaces.Mail)
	a.True(ok)
	a.NotNil(mail)

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
	a.Nil(err)

	time.Sleep(time.Second * 10)

	err = p.Stop(nil, nil)
	a.Nil(err)
	a.Nil(p.Instance())
}

func stringFunc(s string) cfg.String {
	return func() string { return s }
}

func boolFunc(b bool) cfg.Bool {
	return func() bool { return b }
}

func intFunc(i int) cfg.Int {
	return func() int { return i }
}
