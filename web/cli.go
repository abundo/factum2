package web

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"

	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
	"golang.org/x/term"
	"gorm.io/gorm"
)

var (
	adminUsername = "admin"
	adminName     = "Administrator"
	adminRoleName = "admin"
)

// standardRoles are the roles that must always exist, keyed by
// name -> description.
var standardRoles = map[string]string{
	adminRoleName: "Administrators",
	"operator":    "create, read, update, delete data, except admin",
	"viewer":      "read data",
}

// Function to seed initial data (admin user, roles)
func CreateAdmin(p *GuiParams) error {
	util.Config = &p.Config
	db, err := util.ConnectDatabase(&util.Config.DB)
	if err != nil {
		return err
	}

	for name, description := range standardRoles {
		if _, err := getOrCreateRole(db, name, description); err != nil {
			return err
		}
	}

	fmt.Printf("Creating admin user %s\n", adminUsername)
	adminPassword, err := promptPassword()
	if err != nil {
		return err
	}

	hashedPassword, err := HashPassword(adminPassword)
	if err != nil {
		slog.Error("Failed to hash password for admin user:", "err", err)
	}

	// get admin user if any
	var users []models.User
	res := db.Where("username = ?", adminUsername).Find(&users)
	if res.Error != nil {
		return nil
	}
	if res.RowsAffected > 0 {
		// admin user already exists, just update the password
		existingUser := users[0]
		existingUser.PasswordHash = hashedPassword
		if err := db.Save(&existingUser).Error; err != nil {
			slog.Error("cannot update admin user password", "err", err)
			return err
		}
		slog.Info("Admin user password updated", "username", adminUsername)
		return nil
	}

	adminRole, err := getOrCreateRole(db, adminRoleName, standardRoles[adminRoleName])
	if err != nil {
		return err
	}

	// create admin user and the admin role
	adminUser := models.User{
		Username:     adminUsername,
		PasswordHash: hashedPassword,
		Name:         adminName,
		Roles:        []*models.Role{&adminRole}, // Assign the admin role
	}
	err = db.Create(&adminUser).Error
	if err != nil {
		return err
	}

	return nil
}

// getOrCreateRole returns the role with the given name, creating it with
// the given description if it doesn't already exist.
func getOrCreateRole(db *gorm.DB, name, description string) (models.Role, error) {
	var roles []models.Role
	res := db.Where("name = ?", name).Find(&roles)
	if res.Error != nil {
		return models.Role{}, res.Error
	}
	if res.RowsAffected > 0 {
		return roles[0], nil
	}

	fmt.Printf("Creating role %s\n", name)
	role := models.Role{Name: name, Description: description}
	if err := db.Create(&role).Error; err != nil {
		return models.Role{}, err
	}
	return role, nil
}

// promptPassword prompts twice for a password without echoing it to the
// terminal, and returns it once both entries match.
func promptPassword() (string, error) {
	for {
		fmt.Print("Enter admin password: ")
		pw1, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return "", err
		}

		fmt.Print("Confirm admin password: ")
		pw2, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return "", err
		}

		if !bytes.Equal(pw1, pw2) {
			fmt.Println("Passwords do not match, try again.")
			continue
		}
		if len(pw1) == 0 {
			fmt.Println("Password cannot be empty, try again.")
			continue
		}

		return string(pw1), nil
	}
}
