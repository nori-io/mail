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
	"sync"
	"time"

	cfg "github.com/nori-io/nori-common/config"
	"github.com/nori-io/nori-common/interfaces"
	"github.com/nori-io/nori-common/meta"
	noriPlugin "github.com/nori-io/nori-common/plugin"
	"gopkg.in/gomail.v2"
)

const (
	shutdownTimeout = 5 * time.Second
)

type plugin struct {
	instance interfaces.Mail
	config   config
}

type config struct {
	host           cfg.String
	port           cfg.Int
	user           cfg.String
	password       cfg.String
	ssl            cfg.Bool
	localName      cfg.String
	workerEnabled  cfg.Bool
	workerPoolSize cfg.Int
	queueKey       cfg.String

	subjectFromConfig   cfg.String
	fromEmailFromConfig cfg.String
	fromNameFromConfig  cfg.String
}

type instance struct {
	pubsub interfaces.PubSub
	//logger    logrus.Logger
	logger    interfaces.Logger
	dialer    *gomail.Dialer
	templColl interfaces.Templates
	done      chan struct{}
	queueKey  cfg.String

	beforeSend     []func(interface{})
	afterSend      []func(interface{})
	beforeDelivery []func(interface{})
	afterDelivery  []func(interface{})

	sync.Mutex
}

var (
	Plugin plugin
)

func (p *plugin) Init(_ context.Context, configManager cfg.Manager) error {
	cm := configManager.Register(p.Meta())
	p.config = config{
		host:                cm.String("mail.host", ""),
		port:                cm.Int("mail.port", ""),
		user:                cm.String("mail.user", ""),
		password:            cm.String("mail.password", ""),
		ssl:                 cm.Bool("mail.ssl", ""),
		localName:           cm.String("mail.local_name", ""),
		workerEnabled:       cm.Bool("mail.worker.enabled", ""),
		workerPoolSize:      cm.Int("mail.worker.pool_size", ""),
		queueKey:            cm.String("mail.key", ""),
		subjectFromConfig:   cm.String(subjectFromConfig, "default mail subject"),
		fromEmailFromConfig: cm.String(fromEmailFromConfig, "default from email"),
		fromNameFromConfig:  cm.String(fromNameFromConfig, "default from email"),
	}
	return nil
}

func (p *plugin) Instance() interface{} {
	return p.instance
}

func (p plugin) Meta() meta.Meta {
	return &meta.Data{

		ID: meta.ID{
			ID:      "nori/mail",
			Version: "1.0.0",
		},
		Author: meta.Author{
			Name: "Nori",
			URI:  "https://nori.io",
		},
		Core: meta.Core{
			VersionConstraint: ">=1.0.0, <2.0.0",
		},
		Dependencies: []meta.Dependency{meta.PubSub.Dependency("1.0.0"), meta.Templates.Dependency("1.0.0")},
		Description: meta.Description{
			Name: "Nori: Mail",
		},
		Interface: meta.Mail,
		License: meta.License{
			Title: "",
			Type:  "GPLv3",
			URI:   "https://www.gnu.org/licenses/",
		},
		Tags: []string{"mail"},
	}

}

func (p *plugin) Start(ctx context.Context, registry noriPlugin.Registry) error {
	if p.instance == nil {
		dialer := gomail.NewPlainDialer(p.config.host(), p.config.port(), p.config.user(), p.config.password())
		dialer.SSL = p.config.ssl()
		pubsub, _ := registry.PubSub()
		templates, _ := registry.Templates()
		instance := &instance{
			queueKey:       p.config.queueKey,
			beforeSend:     make([]func(interface{}), 0),
			afterSend:      make([]func(interface{}), 0),
			beforeDelivery: make([]func(interface{}), 0),
			afterDelivery:  make([]func(interface{}), 0),
			pubsub:         pubsub,
			logger:         registry.Logger(p.Meta()),
			dialer:         dialer,
			templColl:      templates,
			done:           make(chan struct{}),
		}

		p.instance = instance

		if p.config.workerEnabled() {
			go func() {
				for err := range instance.pubsub.Subscriber().Errors() {
					instance.logger.Error(err)
				}
			}()

			mailWorker := instance.newWorkerService(p.config)
			msg := instance.pubsub.Subscriber().Start()

			go func() {
				num := p.config.workerPoolSize()
				for i := 1; i <= num; i++ {
					go func(j int) {
						for {
							select {
							case m := <-msg:
								mailWorker.Publish(m)
								m.Done()
							case <-instance.done:
								return
							}
						}
					}(i)
				}
			}()
		}
	}
	return nil
}

func (p *plugin) Stop(_ context.Context, _ noriPlugin.Registry) error {
	p.instance = nil
	return nil
}

func (i *instance) RegisterBeforeSend(f func(interface{})) {
	i.Lock()
	i.beforeSend = append(i.beforeSend, f)
	i.Unlock()
}

func (i *instance) RegisterAfterSend(f func(interface{})) {
	i.Lock()
	i.afterSend = append(i.afterSend, f)
	i.Unlock()
}

func (i *instance) RegisterBeforeDelivery(f func(interface{})) {
	i.Lock()
	i.beforeDelivery = append(i.beforeDelivery, f)
	i.Unlock()
}

func (i *instance) RegisterAfterDelivery(f func(interface{})) {
	i.Lock()
	i.afterDelivery = append(i.afterDelivery, f)
	i.Unlock()
}

func (i *instance) Send(msg interface{}) error {
	for _, f := range i.beforeSend {
		f(msg)
	}

	err := i.pubsub.Publisher().Publish(context.Background(), i.queueKey(), msg)

	for _, f := range i.afterSend {
		f(msg)
	}
	return err
}
