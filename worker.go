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
	"errors"
	"time"

	"github.com/golang/protobuf/ptypes"
	"gopkg.in/gomail.v2"

	"github.com/nori-io/nori-common/interfaces"

	"github.com/nori-io/mail/message"
)

type mailWorker struct {
	mailChannel chan mailChan
	templColl   interfaces.Templates
	logger      interfaces.Logger
	cfg         config
}

type mailChan struct {
	msg *gomail.Message
}

const (
	subjectKey   = "subject"
	fromEmailKey = "from_email"
	fromNameKey  = "from_name"

	subjectFromConfig   = "mail.cfg.subject"
	fromEmailFromConfig = "mail.cfg.from.email"
	fromNameFromConfig  = "mail.cfg.from.name"
)

func (i instance) newWorkerService(cfg config) *mailWorker {
	worker := new(mailWorker)
	worker.mailChannel = make(chan mailChan)
	worker.cfg = cfg
	worker.templColl = i.templColl
	worker.logger = i.logger

	go func() {
		var sender gomail.SendCloser
		var err error
		open := false
		for {
			select {
			case mailMessage, ok := <-worker.mailChannel:
				if !ok {
					return
				}
				if !open {
					var count int = 1
					for sender == nil {
						sender, err = i.dialer.Dial()
						i.logger.Info("Connection open")
						if err != nil {
							if count <= 5 {
								worker.logger.Errorf("attempt number = %d, %v", count, err)
								count++
								time.Sleep(3 * time.Second)
							} else {
								worker.logger.Error(err)
							}
						}
					}
					open = true
				}
				if err := gomail.Send(sender, mailMessage.msg); err != nil {
					worker.logger.Error(err)
				}
			case <-time.After(30 * time.Second):
				if open {
					if err := sender.Close(); err != nil {
						worker.logger.Error(err)
					}
					open = false
					i.logger.Info("Connection close")
					sender = nil
				}
			}
		}
	}()
	return worker
}

func (w mailWorker) Publish(m interfaces.Message) error {
	msg := new(message.Message)
	if err := m.UnMarshal(msg); err != nil {
		w.logger.Errorf("Protobuf unmarshall error: %v", err)
		return err
	}

	timestamp, err := ptypes.Timestamp(msg.Timestamp)
	if err != nil {
		w.logger.Errorf("error: %v, message_id: %d, user_id: %d, tracing_id: %v, service_name: %s, template: %s",
			err, msg.ID, msg.UserID, msg.TracingID, msg.ServiceName, msg.TemplateName)
		return err
	}

	ttl, err := ptypes.Duration(msg.TTL)
	if err != nil {
		w.logger.Errorf("error: %v, message_id: %d, user_id: %d, tracing_id: %v, service_name: %s, template: %s",
			err, msg.ID, msg.UserID, msg.TracingID, msg.ServiceName, msg.TemplateName)
		return err
	}

	deadline := timestamp.Add(ttl)

	if time.Now().After(deadline) && ttl > 0 {
		err = errors.New("message expired")
		w.logger.Errorf("error: %v, message_id: %d, user_id: %d, tracing_id: %v, created_at: %t, ttl: %v, service_name: %s, template: %s",
			err, msg.ID, msg.UserID, msg.TracingID, timestamp, ttl, msg.ServiceName, msg.TemplateName)

		return err
	}

	emailBody, err := w.templColl.Execute(msg.TemplateName, msg.Variables)
	if err != nil {
		w.logger.Errorf("error: %v, message_id: %d, user_id: %d, tracing_id: %v, created_at: %t, ttl: %v, service_name: %s, template: %s",
			err, msg.ID, msg.UserID, msg.TracingID, timestamp, ttl, msg.ServiceName, msg.TemplateName)
		return err
	}

	var subject, fromEmail, fromName string
	var ok bool
	subject, ok = msg.Variables[subjectKey]
	if !ok {
		subject = w.cfg.subjectFromConfig()
	}

	fromEmail, ok = msg.Variables[fromEmailKey]
	if !ok {
		subject = w.cfg.fromEmailFromConfig()
	}

	fromName, ok = msg.Variables[fromNameKey]
	if !ok {
		subject = w.cfg.fromNameFromConfig()
	}

	emailMessage := gomail.NewMessage()
	emailMessage.SetBody("text/html", emailBody)
	emailMessage.SetHeader("Subject", subject)
	emailMessage.SetAddressHeader("To", msg.Email, "")
	emailMessage.SetAddressHeader(
		"From",
		fromEmail,
		fromName,
	)
	emailMessage.SetDateHeader("X-Date", timestamp)

	w.mailChannel <- mailChan{emailMessage}
	return nil
}
