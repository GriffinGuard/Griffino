package system

import "fmt"

// ContainerConflictError 表示容器名与非 Griffino 容器冲突
type ContainerConflictError struct {
    Name string
    ID   string
}

func (e *ContainerConflictError) Error() string {
    return fmt.Sprintf("container name %s is already in use by a non-Griffino container (ID: %s)", e.Name, e.ID)
}