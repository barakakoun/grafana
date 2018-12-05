package alert_notifications

import (
	"errors"

	"github.com/grafana/grafana/pkg/bus"
	"github.com/grafana/grafana/pkg/log"
	"github.com/grafana/grafana/pkg/models"
)

var (
	ErrInvalidConfigTooManyDefault = errors.New("Alert notification provisioning config is invalid. Only one alert notification can be marked as default")
	ErrInvalidNotifierType         = errors.New("Unknown notifier type")
)

func Provision(configDirectory string) error {
	dc := newNotificationProvisioner(log.New("provisioning.alert_notifications"))
	return dc.applyChanges(configDirectory)
}

type NotificationProvisioner struct {
	log         log.Logger
	cfgProvider *configReader
}

func newNotificationProvisioner(log log.Logger) NotificationProvisioner {
	return NotificationProvisioner{
		log:         log,
		cfgProvider: &configReader{log: log},
	}
}

func (dc *NotificationProvisioner) apply(cfg *notificationsAsConfig) error {
	if err := dc.deleteNotifications(cfg.DeleteNotifications); err != nil {
		return err
	}

	if err := dc.mergeNotifications(cfg.Notifications); err != nil {
		return err
	}

	return nil
}

func (dc *NotificationProvisioner) deleteNotifications(notificationToDelete []*deleteNotificationConfig) error {
	for _, notification := range notificationToDelete {
		dc.log.Info("Deleting alert notification", "name", notification.Name)

		if notification.OrgId == 0 && notification.OrgName != "" {
			getOrg := &models.GetOrgByNameQuery{Name: notification.OrgName}
			if err := bus.Dispatch(getOrg); err != nil {
				return err
			}
			notification.OrgId = getOrg.Result.Id
		} else if notification.OrgId < 0 {
			notification.OrgId = 1
		}
		getNotification := &models.GetAlertNotificationsQuery{Name: notification.Name, OrgId: notification.OrgId}

		if err := bus.Dispatch(getNotification); err != nil {
			return err
		}

		if getNotification.Result != nil {
			cmd := &models.DeleteAlertNotificationCommand{Id: getNotification.Result.Id, OrgId: getNotification.OrgId}
			if err := bus.Dispatch(cmd); err != nil {
				return err
			}
		}
	}

	return nil
}

func (dc *NotificationProvisioner) mergeNotifications(notificationToMerge []*notificationFromConfig) error {
	for _, notification := range notificationToMerge {

		if notification.OrgId == 0 && notification.OrgName != "" {
			getOrg := &models.GetOrgByNameQuery{Name: notification.OrgName}
			if err := bus.Dispatch(getOrg); err != nil {
				return err
			}
			notification.OrgId = getOrg.Result.Id
		} else if notification.OrgId < 0 {
			notification.OrgId = 1
		}

		cmd := &models.GetAlertNotificationsQuery{OrgId: notification.OrgId, Name: notification.Name}
		err := bus.Dispatch(cmd)
		if err != nil {
			return err
		}

		if cmd.Result == nil {
			dc.log.Info("Inserting alert notification from configuration ", "name", notification.Name)
			insertCmd := &models.CreateAlertNotificationCommand{
				Name:      notification.Name,
				Type:      notification.Type,
				IsDefault: notification.IsDefault,
				Settings:  notification.SettingsToJson(),
				OrgId:     notification.OrgId,
			}
			if err := bus.Dispatch(insertCmd); err != nil {
				return err
			}
		} else {
			dc.log.Info("Updating alert notification from configuration", "name", notification.Name)
			updateCmd := &models.UpdateAlertNotificationCommand{
				Id:        cmd.Result.Id,
				Name:      notification.Name,
				Type:      notification.Type,
				IsDefault: notification.IsDefault,
				Settings:  notification.SettingsToJson(),
				OrgId:     notification.OrgId,
			}
			if err := bus.Dispatch(updateCmd); err != nil {
				return err
			}
		}
	}

	return nil
}

func (cfg *notificationsAsConfig) mapToNotificationFromConfig() *notificationsAsConfig {
	r := &notificationsAsConfig{}
	if cfg == nil {
		return r
	}

	for _, notification := range cfg.Notifications {
		r.Notifications = append(r.Notifications, &notificationFromConfig{
			OrgId:     notification.OrgId,
			OrgName:   notification.OrgName,
			Name:      notification.Name,
			Type:      notification.Type,
			IsDefault: notification.IsDefault,
			Settings:  notification.Settings,
		})
	}

	for _, notification := range cfg.DeleteNotifications {
		r.DeleteNotifications = append(r.DeleteNotifications, &deleteNotificationConfig{
			OrgId:   notification.OrgId,
			OrgName: notification.OrgName,
			Name:    notification.Name,
		})
	}

	return r
}

func (dc *NotificationProvisioner) applyChanges(configPath string) error {
	configs, err := dc.cfgProvider.readConfig(configPath)
	if err != nil {
		return err
	}

	for _, cfg := range configs {
		if err := dc.apply(cfg); err != nil {
			return err
		}
	}

	return nil
}
