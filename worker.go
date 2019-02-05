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
	"github.com/sirupsen/logrus"
	"gopkg.in/gomail.v2"

	"github.com/nori-io/mail/message"
	"github.com/nori-io/nori-common/interfaces"
)

type mailWorker struct {
	mailChannel chan mailChan
	templColl   interfaces.Templates
	logger      *logrus.Logger
	cfg         config
}

type mailChan struct {
	msg    *gomail.Message
	fields logrus.Fields
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
						i.logger.WithField("component", "sender").Info("Connection open")
						if err != nil {
							mailMessage.fields["component"] = "MailDialer.Dial"
							if count <= 5 {
								mailMessage.fields["attempt number"] = count
								worker.logger.WithFields(mailMessage.fields).Error(err)
								count++
								time.Sleep(3 * time.Second)
							} else {
								worker.logger.WithFields(mailMessage.fields).Fatal(err)
							}
						}
					}
					open = true
				}
				mailMessage.fields["component"] = "gomail.Send"
				if err := gomail.Send(sender, mailMessage.msg); err != nil {
					worker.logger.WithFields(mailMessage.fields).Error(err)
				} else {
					worker.logger.WithFields(mailMessage.fields).Info()
				}
			case <-time.After(30 * time.Second):
				if open {
					if err := sender.Close(); err != nil {
						worker.logger.Error(err)
					}
					open = false
					i.logger.WithField("component", "sender").Info("Connection close")
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
		w.logger.WithFields(logrus.Fields{"component": "protobuf unmarshal"}).Error(err)
		return err
	}

	timestamp, err := ptypes.Timestamp(msg.Timestamp)
	if err != nil {
		w.logger.WithFields(logrus.Fields{
			"component":    "protobuf timestamp",
			"message_id":   msg.ID,
			"user_id":      msg.UserID,
			"tracing_id":   msg.TracingID,
			"service_name": msg.ServiceName,
			"template":     msg.TemplateName,
		}).Error(err)
		return err
	}

	ttl, err := ptypes.Duration(msg.TTL)
	if err != nil {
		w.logger.WithFields(logrus.Fields{
			"component":    "protobuf duration",
			"message_id":   msg.ID,
			"user_id":      msg.UserID,
			"tracing_id":   msg.TracingID,
			"created_at":   timestamp,
			"service_name": msg.ServiceName,
			"template":     msg.TemplateName,
		}).Error(err)
		return err
	}

	deadline := timestamp.Add(ttl)

	if time.Now().After(deadline) && ttl > 0 {
		err = errors.New("message expired")
		w.logger.WithFields(logrus.Fields{
			"message_id":   msg.ID,
			"user_id":      msg.UserID,
			"tracing_id":   msg.TracingID,
			"created_at":   timestamp,
			"ttl":          ttl,
			"service_name": msg.ServiceName,
			"template":     msg.TemplateName,
		}).Error(err)
		return err
	}

	emailBody, err := w.templColl.Execute(msg.TemplateName, msg.Variables)
	if err != nil {
		w.logger.WithFields(logrus.Fields{
			"component":    "template execute",
			"message_id":   msg.ID,
			"user_id":      msg.UserID,
			"tracing_id":   msg.TracingID,
			"created_at":   timestamp,
			"ttl":          ttl,
			"service_name": msg.ServiceName,
			"template":     msg.TemplateName,
		}).Error(err)
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

	fs := logrus.Fields{
		"message_id":   msg.ID,
		"user_id":      msg.UserID,
		"tracing_id":   msg.TracingID,
		"created_at":   timestamp,
		"ttl":          ttl,
		"service_name": msg.ServiceName,
		"template":     msg.TemplateName,
	}
	w.mailChannel <- mailChan{emailMessage, fs}
	return nil
}
