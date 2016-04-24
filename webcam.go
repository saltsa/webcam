// Library for working with webcams and other video capturing devices.
// It depends entirely on v4l2 framework, thus will compile and work
// only on Linux machine
package webcam

/*
#include <stdint.h>
#include <stdlib.h>
#include "webcam.h"
*/
import "C"
import "unsafe"
import "errors"

type buffer struct {
	start  unsafe.Pointer
	length uint32
}

// Webcam object
type Webcam struct {
	fd      int
	buffers []buffer
}

// Open a webcam with a given path
// Checks if device is a v4l2 device and if it is
// capable to stream video
func Open(path string) (*Webcam, error) {
	cpath := C.CString(path)
	fd, err := C.openWebcam(cpath)
	C.free(unsafe.Pointer(cpath))

	if fd < 0 {
		return nil, err
	}

	var is_video_device, can_stream C.int
	res, err := C.checkCapabilities(fd, &is_video_device, &can_stream)

	if res < 0 {
		return nil, err
	}

	if int(is_video_device) == 0 {
		return nil, errors.New("Not a video capture device")
	}

	if int(can_stream) == 0 {
		return nil, errors.New("Device does not support the streaming I/O method")
	}

	w := new(Webcam)
	w.fd = int(fd)
	return w, nil
}

// Returns image formats supported by the device alongside with
// their text description
// Not that this function is somewhat experimental. Frames are not ordered in
// any meaning, also duplicates can occur so it's up to developer to clean it up.
// See http://linuxtv.org/downloads/v4l-dvb-apis/vidioc-enum-framesizes.html
// for more information
func (w *Webcam) GetSupportedFormats() map[PixelFormat]string {
	result := make(map[PixelFormat]string)

	var desc [32]C.char
	var code C.uint32_t

	for index := 0; C.getPixelFormat(C.int(w.fd), C.int(index), &code, &desc[0]) == 0; index++ {
		result[PixelFormat(code)] = C.GoString(&desc[0])
	}

	return result
}

// Returns supported frame sizes for a given image format
func (w *Webcam) GetSupportedFrameSizes(f PixelFormat) []FrameSize {
	result := make([]FrameSize, 0)

	var sizes [6]C.uint32_t

	for index := 0; C.getFrameSize(C.int(w.fd), C.int(index), C.uint32_t(f), &sizes[0]) == 0; index++ {
		var s FrameSize
		s.MinWidth = uint32(sizes[0])
		s.MaxWidth = uint32(sizes[1])
		s.StepWidth = uint32(sizes[2])
		s.MinHeight = uint32(sizes[3])
		s.MaxHeight = uint32(sizes[4])
		s.StepHeight = uint32(sizes[5])
		result = append(result, s)
	}

	return result
}

// Sets desired image format and frame size
// Note, that device driver can change that values.
// Resulting values are returned by a function
// alongside with an error if any
func (w *Webcam) SetImageFormat(f PixelFormat, width, height uint32) (PixelFormat, uint32, uint32, error) {

	code := C.uint32_t(f)
	cw := C.uint32_t(width)
	ch := C.uint32_t(height)

	res, err := C.setImageFormat(C.int(w.fd), &code, &cw, &ch)
	if res < 0 {
		return 0, 0, 0, err
	} else {
		return PixelFormat(code), uint32(cw), uint32(ch), nil
	}
}

// Start streaming process
func (w *Webcam) StartStreaming() error {

	buf_count := C.uint32_t(256)

	res, err := C.mmapRequestBuffers(C.int(w.fd), &buf_count)
	if res < 0 {
		return err
	}

	if uint32(buf_count) < 2 {
		return errors.New("Insufficient buffer memory")
	}

	w.buffers = make([]buffer, uint32(buf_count))

	for index, buf := range w.buffers {
		var length C.uint32_t
		var start unsafe.Pointer

		res, err := C.mmapQueryBuffer(C.int(w.fd), C.uint32_t(index), &length, &start)

		if res < 0 {
			if err != nil {
				return err
			} else {
				return errors.New("Failed to map memory")
			}
		}

		buf.start = start
		buf.length = uint32(length)
		w.buffers[index] = buf
	}

	for index, _ := range w.buffers {
		res, err = C.mmapEnqueueBuffer(C.int(w.fd), C.uint32_t(index))
		if res < 0 {
			return errors.New("Failed to enqueue buffer")
		}
	}

	res, err = C.startStreaming(C.int(w.fd))
	if res < 0 {
		return err
	}

	return nil
}

// Read a single frame from the webcam
// If frame cannot be read at the moment
// function will return empty array
func (w *Webcam) ReadFrame() ([]byte, error) {
	var index C.uint32_t
	var length C.uint32_t
	res, err := C.mmapDequeueBuffer(C.int(w.fd), &index, &length)

	if res < 0 {
		return nil, err
	} else if res > 0 {
		return nil, nil
	}

	buffer := w.buffers[int(index)]
	result := C.GoBytes(buffer.start, C.int(length))

	res, err = C.mmapEnqueueBuffer(C.int(w.fd), index)

	if res < 0 {
		return result, err
	} else {
		return result, nil
	}
}

// Wait until frame could be read
func (w *Webcam) WaitForFrame(timeout uint32) error {
	res, err := C.waitForFrame(C.int(w.fd), C.uint32_t(timeout))
	if res < 0 {
		return err
	} else if res == 0 {
		return new(Timeout)
	} else {
		return nil
	}
}


func (w *Webcam) StopStreaming() error {
	for _, buffer := range w.buffers {
		res, err := C.mmapReleaseBuffer(buffer.start, C.uint32_t(buffer.length))
		if res < 0 {
			return err
		}
	}

	res, err := C.stopStreaming(C.int(w.fd))
  if res < 0 {
    return err
  }

  return nil
}

// Close the device
func (w *Webcam) Close() error {
  w.StopStreaming()

	res, err := C.closeWebcam(C.int(w.fd))
	if res < 0 {
		return err
	}

	return nil
}
